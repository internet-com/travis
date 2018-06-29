package stake

import (
	"bytes"
	"math/big"
	"sort"

	"github.com/ethereum/go-ethereum/common"
	abci "github.com/tendermint/abci/types"

	"encoding/json"
	"github.com/CyberMiles/travis/sdk/state"
	"github.com/CyberMiles/travis/types"
	"github.com/CyberMiles/travis/utils"
	"github.com/tendermint/go-crypto"
	"golang.org/x/crypto/ripemd160"
)

//_________________________________________________________________________

// Candidate defines the total Amount of bond shares and their exchange rate to
// coins. Accumulation of interest is modelled as an in increase in the
// exchange rate, and slashing as a decrease.  When coins are delegated to this
// candidate, the candidate is credited with a DelegatorBond whose number of
// bond shares is based on the Amount of coins delegated divided by the current
// exchange rate. Voting power can be calculated as total bonds multiplied by
// exchange rate.
// NOTE if the Owner.Empty() == true then this is a candidate who has revoked candidacy
type Candidate struct {
	PubKey       types.PubKey `json:"pub_key"`       // Pubkey of candidate
	OwnerAddress string       `json:"owner_address"` // Sender of BondTx - UnbondTx returns here
	Shares       string       `json:"shares"`        // Total number of delegated shares to this candidate, equivalent to coins held in bond account
	VotingPower  int64        `json:"voting_power"`  // Voting power if pubKey is a considered a validator
	MaxShares    string       `json:"max_shares"`
	CompRate     string       `json:"comp_rate"`
	CreatedAt    string       `json:"created_at"`
	UpdatedAt    string       `json:"updated_at"`
	Description  Description  `json:"description"`
	Verified     string       `json:"verified"`
	Active       string       `json:"active"`
	BlockHeight  int64        `json:"block_height"`
	Rank         int64        `json:"rank"`
	State        string       `json:"state"`
}

type Description struct {
	Name     string `json:"name"`
	Website  string `json:"website"`
	Location string `json:"location"`
	Email    string `json:"email"`
	Profile  string `json:"profile"`
}

// Validator returns a copy of the Candidate as a Validator.
// Should only be called when the Candidate qualifies as a validator.
func (c *Candidate) Validator() Validator {
	return Validator(*c)
}

func (c *Candidate) ParseShares() *big.Int {
	return utils.ParseInt(c.Shares)
}

func (c *Candidate) ParseMaxShares() *big.Int {
	return utils.ParseInt(c.MaxShares)
}

func (c *Candidate) ParseCompRate() float64 {
	return utils.ParseFloat(c.CompRate)
}

func (c *Candidate) AddShares(value *big.Int) *big.Int {
	x := new(big.Int)
	x.Add(c.ParseShares(), value)
	c.Shares = x.String()
	return x
}

func (c *Candidate) SelfStakingAmount(ratio string) (result *big.Int) {
	result = new(big.Int)
	z := new(big.Float)
	maxShares := new(big.Float).SetInt(c.ParseMaxShares())
	r, _ := new(big.Float).SetString(ratio)
	z.Mul(maxShares, r)
	z.Int(result)
	return
}

func (c *Candidate) Hash() []byte {
	candidate, err := json.Marshal(struct {
		PubKey       types.PubKey
		OwnerAddress string
		Shares       string
		VotingPower  int64
		MaxShares    string
		CompRate     string
		Description  Description
		Verified     string
		Active       string
	}{
		c.PubKey,
		c.OwnerAddress,
		c.Shares,
		c.VotingPower,
		c.MaxShares,
		c.CompRate,
		Description{
			Name:     c.Description.Name,
			Website:  c.Description.Website,
			Location: c.Description.Location,
			Profile:  c.Description.Profile,
			Email:    c.Description.Email,
		},
		c.Verified,
		c.Active,
	})
	if err != nil {
		panic(err)
	}
	hasher := ripemd160.New()
	hasher.Write(candidate)
	return hasher.Sum(nil)
}

// Validator is one of the top Candidates
type Validator Candidate

// ABCIValidator - Get the validator from a bond value
func (v Validator) ABCIValidator() abci.Validator {
	pk := v.PubKey.PubKey.(crypto.PubKeyEd25519)
	return abci.Validator{
		PubKey: abci.PubKey{
			Type: abci.PubKeyEd25519,
			Data: pk[:],
		},
		Power: v.VotingPower,
	}
}

//_________________________________________________________________________

type Candidates []*Candidate

var _ sort.Interface = Candidates{} //enforce the sort interface at compile time

// nolint - sort interface functions
func (cs Candidates) Len() int      { return len(cs) }
func (cs Candidates) Swap(i, j int) { cs[i], cs[j] = cs[j], cs[i] }
func (cs Candidates) Less(i, j int) bool {
	vp1, vp2 := cs[i].VotingPower, cs[j].VotingPower
	//pk1, pk2 := cs[i].PubKey.Bytes(), cs[j].PubKey.Bytes()
	pk1, pk2 := cs[i].PubKey.Address(), cs[j].PubKey.Address()

	//note that all ChainId and App must be the same for a group of candidates
	if vp1 != vp2 {
		return vp1 > vp2
	}
	return bytes.Compare(pk1, pk2) == -1
}

// Sort - Sort the array of bonded values
func (cs Candidates) Sort() {
	sort.Sort(cs)
}

// update the voting power and save
func (cs Candidates) updateVotingPower(store state.SimpleDB) Candidates {

	// update voting power
	for _, c := range cs {
		shares := c.ParseShares()
		if c.Active == "N" {
			c.VotingPower = 0
		} else if big.NewInt(c.VotingPower).Cmp(shares) != 0 {
			v := new(big.Int)
			v.Div(shares, big.NewInt(1e18))
			c.VotingPower = v.Int64()
		}
	}
	cs.Sort()
	for i, c := range cs {
		// truncate the power
		if i >= int(utils.GetParams().MaxVals) {
			c.VotingPower = 0
		}
		updateCandidate(c)
	}
	return cs
}

// Validators - get the most recent updated validator set from the
// Candidates. These bonds are already sorted by VotingPower from
// the UpdateVotingPower function which is the only function which
// is to modify the VotingPower
func (cs Candidates) Validators() Validators {

	//test if empty
	if len(cs) == 1 {
		if cs[0].VotingPower == 0 {
			return nil
		}
	}

	validators := make(Validators, len(cs))
	for i, c := range cs {
		if c.VotingPower == 0 { //exit as soon as the first Voting power set to zero is found
			return validators[:i]
		}
		validators[i] = c.Validator()
	}

	return validators
}

//_________________________________________________________________________

// Validators - list of Validators
type Validators []Validator

var _ sort.Interface = Validators{} //enforce the sort interface at compile time

// nolint - sort interface functions
func (vs Validators) Len() int      { return len(vs) }
func (vs Validators) Swap(i, j int) { vs[i], vs[j] = vs[j], vs[i] }
func (vs Validators) Less(i, j int) bool {
	//pk1, pk2 := vs[i].PubKey.Bytes(), vs[j].PubKey.Bytes()
	//return bytes.Compare(pk1, pk2) == -1

	pk1, pk2 := vs[i].PubKey, vs[j].PubKey
	return bytes.Compare(pk1.Address(), pk2.Address()) == -1
}

// Sort - Sort validators by pubkey
func (vs Validators) Sort() {
	sort.Sort(vs)
}

// determine all changed validators between two validator sets
func (vs Validators) validatorsChanged(vs2 Validators) (changed []abci.Validator) {

	//first sort the validator sets
	vs.Sort()
	vs2.Sort()

	max := len(vs) + len(vs2)
	changed = make([]abci.Validator, max)
	i, j, n := 0, 0, 0 //counters for vs loop, vs2 loop, changed element

	for i < len(vs) && j < len(vs2) {

		if !vs[i].PubKey.Equals(vs2[j].PubKey) {
			// pk1 > pk2, a new validator was introduced between these pubkeys
			//if bytes.Compare(vs[i].PubKey.Bytes(), vs2[j].PubKey.Bytes()) == 1 {
			if bytes.Compare(vs[i].PubKey.Address(), vs2[j].PubKey.Address()) == 1 {
				changed[n] = vs2[j].ABCIValidator()
				n++
				j++
				continue
			} // else, the old validator has been removed
			pk := vs[i].PubKey.PubKey.(crypto.PubKeyEd25519)
			changed[n] = abci.Ed25519Validator(pk[:], 0)
			n++
			i++
			continue
		}

		if vs[i].VotingPower != vs2[j].VotingPower {
			changed[n] = vs2[j].ABCIValidator()
			n++
		}
		j++
		i++
	}

	// add any excess validators in set 2
	for ; j < len(vs2); j, n = j+1, n+1 {
		changed[n] = vs2[j].ABCIValidator()
	}

	// remove any excess validators left in set 1
	for ; i < len(vs); i, n = i+1, n+1 {
		pk := vs[i].PubKey.PubKey.(crypto.PubKeyEd25519)
		changed[n] = abci.Ed25519Validator(pk[:], 0)
	}

	return changed[:n]
}

func (vs Validators) Remove(i int) Validators {
	copy(vs[i:], vs[i+1:])
	return vs[:len(vs)-1]
}

// UpdateValidatorSet - Updates the voting power for the candidate set and
// returns the subset of validators which have changed for Tendermint
func UpdateValidatorSet(store state.SimpleDB) (change []abci.Validator, err error) {

	// get the validators before update
	candidates := GetCandidates()
	candidates.Sort()

	v1 := candidates.Validators()
	v2 := candidates.updateVotingPower(store).Validators()

	change = v1.validatorsChanged(v2)

	// clean all of the candidates had been withdrawed
	cleanCandidates()
	return
}

//_________________________________________________________________________

type Delegator struct {
	Address   common.Address
	CreatedAt string
}

type Delegation struct {
	DelegatorAddress common.Address `json:"delegator_address"`
	PubKey           types.PubKey   `json:"pub_key"`
	DelegateAmount   string         `json:"delegate_amount"`
	AwardAmount      string         `json:"award_amount"`
	WithdrawAmount   string         `json:"withdraw_amount"`
	SlashAmount      string         `json:"slash_amount"`
	CreatedAt        string         `json:"created_at"`
	UpdatedAt        string         `json:"updated_at"`
}

func (d *Delegation) Shares() (shares *big.Int) {
	shares = new(big.Int)
	shares.Add(d.ParseDelegateAmount(), d.ParseAwardAmount())
	shares.Sub(shares, d.ParseWithdrawAmount())
	shares.Sub(shares, d.ParseSlashAmount())
	return
}

func (d *Delegation) ParseDelegateAmount() *big.Int {
	return utils.ParseInt(d.DelegateAmount)
}

func (d *Delegation) ParseAwardAmount() *big.Int {
	return utils.ParseInt(d.AwardAmount)
}

func (d *Delegation) ParseWithdrawAmount() *big.Int {
	return utils.ParseInt(d.WithdrawAmount)
}

func (d *Delegation) ParseSlashAmount() *big.Int {
	return utils.ParseInt(d.SlashAmount)
}

func (d *Delegation) AddDelegateAmount(value *big.Int) *big.Int {
	x := new(big.Int)
	x.Add(d.ParseDelegateAmount(), value)
	d.DelegateAmount = x.String()
	return x
}

func (d *Delegation) AddAwardAmount(value *big.Int) *big.Int {
	x := new(big.Int)
	x.Add(d.ParseAwardAmount(), value)
	d.AwardAmount = x.String()
	return x
}

func (d *Delegation) AddWithdrawAmount(value *big.Int) *big.Int {
	x := new(big.Int)
	x.Add(d.ParseWithdrawAmount(), value)
	d.WithdrawAmount = x.String()
	return x
}

func (d *Delegation) AddSlashAmount(value *big.Int) *big.Int {
	x := new(big.Int)
	x.Add(d.ParseSlashAmount(), value)
	d.SlashAmount = x.String()
	return x
}

func (d *Delegation) Hash() []byte {
	delegation, err := json.Marshal(struct {
		DelegatorAddress common.Address
		PubKey           types.PubKey
		DelegateAmount   string
		AwardAmount      string
		WithdrawAmount   string
		SlashAmount      string
	}{
		d.DelegatorAddress,
		d.PubKey,
		d.DelegateAmount,
		d.AwardAmount,
		d.WithdrawAmount,
		d.SlashAmount,
	})
	if err != nil {
		panic(err)
	}
	hasher := ripemd160.New()
	hasher.Write(delegation)
	return hasher.Sum(nil)
}

type DelegateHistory struct {
	Id               int64
	DelegatorAddress common.Address
	PubKey           types.PubKey
	Amount           *big.Int
	OpCode           string
	CreatedAt        string
}

type PunishHistory struct {
	PubKey        types.PubKey
	SlashingRatio float64
	SlashAmount   *big.Int
	Reason        string
	CreatedAt     string
}

type UnstakeRequest struct {
	Id                   string
	DelegatorAddress     common.Address
	PubKey               types.PubKey
	InitiatedBlockHeight int64
	PerformedBlockHeight int64
	Amount               string
	State                string
	CreatedAt            string
	UpdatedAt            string
}

func (r *UnstakeRequest) GenId() []byte {
	req, err := json.Marshal(struct {
		DelegatorAddress     common.Address
		PubKey               types.PubKey
		InitiatedBlockHeight int64
		PerformedBlockHeight int64
		Amount               string
	}{
		r.DelegatorAddress,
		r.PubKey,
		r.InitiatedBlockHeight,
		r.PerformedBlockHeight,
		r.Amount,
	})

	if err != nil {
		panic(err)
	}

	hasher := ripemd160.New()
	hasher.Write(req)
	return hasher.Sum(nil)
}
