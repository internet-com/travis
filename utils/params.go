package utils

import (
	"reflect"
	"strconv"

	"github.com/ethereum/go-ethereum/common"
	"github.com/CyberMiles/travis/types"
)

type Params struct {
	HoldAccount      common.Address `json:"hold_account"` // PubKey where all bonded coins are held
	MaxVals              uint16     `json:"max_vals"`     // maximum number of validators
	SelfStakingRatio     string     `json:"self_staking_ratio"`
	InflationRate        int64      `json:"inflation_rate"`
	StakeLimit           string     `json:"stake_limit"`
	UnstakeWaitPeriod    uint64      `json:"unstake_wait_period"`
	ProposalExpirePeriod uint64     `json:"proposal_expire_period"`

	DeclareCandidacy	uint64		`json:"declare_candidacy"`
	UpdateCandidacy		uint64		`json:"update_candidacy"`
	GovernancePropose	uint64		`json:"governance_proposal"`
	GasPrice			uint64		`json:"gas_price"`
}

func defaultParams() *Params {
	return &Params{
		HoldAccount:          HoldAccount,
		MaxVals:              100,
		SelfStakingRatio:     "0.1",
		InflationRate:        8,
		StakeLimit:           "0.12",
		UnstakeWaitPeriod:    7 * 24 * 3600 / 10,
		ProposalExpirePeriod: 7 * 24 * 3600 / 10,
		DeclareCandidacy: 		1e6,
		UpdateCandidacy: 		1e6,
		GovernancePropose: 		2e6,
		GasPrice: 				2e9,
	}
}

var (
	// Keys for store prefixes
	ParamKey = []byte{0x01} // key for global parameters
	params = defaultParams()
	dirty = false
)

// load/save the global params
func LoadParams(b []byte) {
	types.Cdc.UnmarshalBinary(b, params)
}

func UnloadParams() (b []byte) {
	b, _ = types.Cdc.MarshalBinary(*params)
	return
}

func GetParams() *Params {
	return params
}

func CleanParams() (before bool) {
	before = dirty
	dirty = false
	return
}

func SetParam(name, value string) bool {
	pv := reflect.ValueOf(params).Elem()
	top := pv.Type()
	for i := 0; i < pv.NumField(); i++ {
		fv := pv.Field(i)
		if top.Field(i).Tag.Get("json") == name {
			switch fv.Kind() {
			case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
				if iv, err := strconv.ParseInt(value, 10, 64); err == nil {
					fv.SetInt(iv)
				}
			case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
				if iv, err := strconv.ParseUint(value, 10, 64); err == nil {
					fv.SetUint(iv)
				}
			case reflect.String:
				fv.SetString(value)
			}
			dirty = true
			return true
		}
	}

	return false
}
