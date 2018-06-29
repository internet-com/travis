package governance

import (
	"bytes"
	"math/big"
	"strings"

	"github.com/CyberMiles/travis/commons"
	"github.com/CyberMiles/travis/modules/stake"
	"github.com/CyberMiles/travis/types"
	"github.com/CyberMiles/travis/utils"
	"github.com/CyberMiles/travis/sdk"
	"github.com/CyberMiles/travis/sdk/state"
	"github.com/ethereum/go-ethereum/common"
	"encoding/json"
)

// nolint
const governanceModuleName = "governance"

// Name is the name of the modules.
func Name() string {
	return governanceModuleName
}

func InitState(module, key, value string, store state.SimpleDB) error {
	return nil
}

func CheckTx(ctx types.Context, store state.SimpleDB,
	tx sdk.Tx) (res sdk.CheckResult, err error) {

	err = tx.ValidateBasic()
	if err != nil {
		return
	}

	// get the sender
	sender, err := getTxSender(ctx)
	if err != nil {
		return
	}

	switch txInner := tx.Unwrap().(type) {
	case TxTransferFundPropose:
		if !bytes.Equal(txInner.Proposer.Bytes(), sender.Bytes()) {
			return sdk.NewCheck(0, ""), ErrMissingSignature()
		}
		validators := stake.GetCandidates().Validators()
		if validators == nil || validators.Len() == 0 {
			return sdk.NewCheck(0, ""), ErrInvalidValidator()
		}
		for i, v := range validators {
			if v.OwnerAddress == txInner.Proposer.String() {
				break
			}
			if i + 1 == len(validators) {
				return sdk.NewCheck(0, ""), ErrInvalidValidator()
			}
		}

		state := ctx.EthappState()
		balance, err := commons.GetBalance(state, *txInner.From)
		if err != nil {
			return sdk.NewCheck(0, ""), ErrInvalidParamerter()
		}

		amount := big.NewInt(0)
		amount.SetString(txInner.Amount, 10)
		if balance.Cmp(amount) < 0 {
			return sdk.NewCheck(0, ""), ErrInsufficientBalance()
		}

		// Transfer gasFee  -- start
		balance, err = commons.GetBalance(state, sender)
		if err != nil {
			return sdk.NewCheck(0, ""), ErrInvalidParamerter()
		}
		params := utils.GetParams()
		gasFee := utils.CalGasFee(params.GovernancePropose, params.GasPrice)

		if balance.Cmp(gasFee) < 0 {
			return sdk.NewCheck(0, ""), ErrInsufficientBalance()
		}
		// Check gasFee  -- end

		utils.TravisTxAddrs = append(utils.TravisTxAddrs, txInner.From)
	case TxChangeParamPropose:
		if !bytes.Equal(txInner.Proposer.Bytes(), sender.Bytes()) {
			return sdk.NewCheck(0, ""), ErrMissingSignature()
		}
		validators := stake.GetCandidates().Validators()
		if validators == nil || validators.Len() == 0 {
			return sdk.NewCheck(0, ""), ErrInvalidValidator()
		}
		for i, v := range validators {
			if v.OwnerAddress == txInner.Proposer.String() {
				break
			}
			if i + 1 == len(validators) {
				return sdk.NewCheck(0, ""), ErrInvalidValidator()
			}
		}
	case TxVote:
		if !bytes.Equal(txInner.Voter.Bytes(), sender.Bytes()) {
			return sdk.NewCheck(0, ""), ErrMissingSignature()
		}

		validators := stake.GetCandidates().Validators()
		if validators == nil || validators.Len() == 0 {
			return sdk.NewCheck(0, ""), ErrInvalidValidator()
		}
		for i, v := range validators {
			if v.OwnerAddress == txInner.Voter.String() {
				break
			}
			if i + 1 == len(validators) {
				return sdk.NewCheck(0, ""), ErrInvalidValidator()
			}
		}

		proposal := GetProposalById(txInner.ProposalId)
		if proposal == nil {
			return sdk.NewCheck(0, ""), ErrInvalidParamerter()
		}
		if proposal.ResultBlockHeight != 0 {
			if proposal.Result == "Approved" {
				return sdk.NewCheck(0, ""), ErrApprovedProposal()
			} else {
				return sdk.NewCheck(0, ""), ErrRejectedProposal()
			}
		}
		if vote := GetVoteByPidAndVoter(txInner.ProposalId, txInner.Voter.String()); vote != nil {
			return sdk.NewCheck(0, ""), ErrRepeatedVote()
		}
		if proposal.Type == TRANSFER_FUND_PROPOSAL {
			utils.TravisTxAddrs = append(utils.TravisTxAddrs, proposal.Detail["to"].(*common.Address))
		}
	}

	return
}

// DeliverTx executes the tx if valid
func DeliverTx(ctx types.Context, store state.SimpleDB,
	tx sdk.Tx, hash []byte) (res sdk.DeliverResult, err error) {

	_, err = CheckTx(ctx, store, tx)
	if err != nil {
		return
	}

	res.GasFee = big.NewInt(0)
	switch txInner := tx.Unwrap().(type) {
	case TxTransferFundPropose:
		expire := utils.GetParams().ProposalExpirePeriod
		if txInner.Expire != 0 {
			expire = txInner.Expire
		}
		hashJson, _ :=	json.Marshal(hash)
		pp := NewTransferFundProposal(
			string(hashJson[1:len(hashJson)-1]),
			txInner.Proposer,
			uint64(ctx.BlockHeight()),
			txInner.From,
			txInner.To,
			txInner.Amount,
			txInner.Reason,
			uint64(ctx.BlockHeight()) + expire,
		)

		state := ctx.EthappState()
		balance, err := commons.GetBalance(state, *txInner.From)
		if err != nil {
			return res, ErrInvalidParamerter()
		}

		amount := big.NewInt(0)
		amount.SetString(txInner.Amount, 10)
		if balance.Cmp(amount) < 0 {
			return res, ErrInsufficientBalance()
		}

		SaveProposal(pp)
		commons.TransferWithReactor(*pp.Detail["from"].(*common.Address), utils.GovHoldAccount, amount, ProposalReactor{pp.Id, uint64(ctx.BlockHeight()), ""})

		// Check gasFee  -- start
		// get the sender
		sender, err := getTxSender(ctx)
		if err != nil {
			return res, err
		}
		balance, err = commons.GetBalance(state, sender)
		if err != nil {
			return res, ErrInvalidParamerter()
		}
		params := utils.GetParams()
		gasFee := utils.CalGasFee(params.GovernancePropose, params.GasPrice)
		res.GasFee = gasFee

		if balance.Cmp(gasFee) < 0 {
			return res, ErrInsufficientBalance()
		}
		res.GasUsed = int64(params.GovernancePropose)
		// transfer gasFee
		commons.Transfer(sender, utils.HoldAccount, gasFee)
		// Check gasFee  -- end

		utils.PendingProposal.Add(pp.Id, pp.ExpireBlockHeight)

		res.Data = hash

	case TxChangeParamPropose:
		expire := utils.GetParams().ProposalExpirePeriod
		if txInner.Expire != 0 {
			expire = txInner.Expire
		}
		hashJson, _ := json.Marshal(hash)
		cp := NewChangeParamProposal(
			string(hashJson[1:len(hashJson)-1]),
			txInner.Proposer,
			uint64(ctx.BlockHeight()),
			txInner.Name,
			txInner.Value,
			txInner.Reason,
			uint64(ctx.BlockHeight()) + expire,
		)
		SaveProposal(cp)

		utils.PendingProposal.Add(cp.Id, cp.ExpireBlockHeight)

		res.Data = hash

	case TxVote:
		vote := NewVote(
			txInner.ProposalId,
			txInner.Voter,
			uint64(ctx.BlockHeight()),
			txInner.Answer,
		)
		SaveVote(vote)

		proposal := GetProposalById(txInner.ProposalId)

		checkResult := CheckProposal(txInner.ProposalId, &txInner.Voter)

		switch proposal.Type {
		case TRANSFER_FUND_PROPOSAL:
			amount := new(big.Int)
			amount.SetString(proposal.Detail["amount"].(string), 10)
			switch checkResult {
			case "approved":
				// as succeeded proposal only need to add balance to receiver,
				// so the transfer should always be successful
				// but we still use the reactor to keep the compatible with the old strategy
				commons.TransferWithReactor(utils.GovHoldAccount, *proposal.Detail["to"].(*common.Address), amount, ProposalReactor{proposal.Id, uint64(ctx.BlockHeight()), "Approved"})
			case "rejected":
				// as succeeded proposal only need to refund balance to sender,
				// so the transfer should always be successful
				// but we still use the reactor to keep the compatible with the old strategy
				commons.TransferWithReactor(utils.GovHoldAccount, *proposal.Detail["from"].(*common.Address), amount, ProposalReactor{proposal.Id, uint64(ctx.BlockHeight()), "Rejected"})
			}
		case CHANGE_PARAM_PROPOSAL:
			switch checkResult {
			case "approved":
				utils.SetParam(proposal.Detail["name"].(string), proposal.Detail["value"].(string))
				ProposalReactor{proposal.Id, uint64(ctx.BlockHeight()), "Approved"}.React("success", "")
			case "rejected":
				ProposalReactor{proposal.Id, uint64(ctx.BlockHeight()), "Rejected"}.React("success", "")
			}
		}
		if checkResult == "approved" || checkResult == "rejected" {
			utils.PendingProposal.Del(proposal.Id)
		}
	}

	return
}

func CheckProposal(pid string, voter *common.Address) string {
	votes := GetVotesByPid(pid)
	validators := stake.GetCandidates().Validators()

	if validators == nil || validators.Len() == 0 {
		return "no validator"
	}

	approvedPower := big.NewInt(0)
	rejectedPower := big.NewInt(0)
	allPower := big.NewInt(0)
	voterPower := big.NewInt(0)
	for _, va := range validators {
		for _, vo := range votes {
			// should check voter is still valid validator first
			if vo.Voter.String() == va.OwnerAddress {
				if strings.Compare(vo.Answer, "Y") == 0 {
					approvedPower.Add(approvedPower, big.NewInt(va.VotingPower))
				}
				if strings.Compare(vo.Answer, "N") == 0 {
					rejectedPower.Add(rejectedPower, big.NewInt(va.VotingPower))
				}
			}
		}
		if voter != nil && voter.String() == va.OwnerAddress {
			voterPower = big.NewInt(va.VotingPower)
		}
		allPower.Add(allPower, big.NewInt(va.VotingPower))
	}

	allPower.Mul(allPower, big.NewInt(2))
	three := big.NewInt(3)
	voterPower.Mul(voterPower, three)
	approvedPower.Mul(approvedPower, three)
	rejectedPower.Mul(rejectedPower, three)

	if approvedPower.Cmp(allPower) >= 0 {
		// To avoid repeated commit, let's recheck with count of voters - voter
		if voter == nil || approvedPower.Sub(approvedPower, voterPower).Cmp(allPower) < 0 {
			return "approved"
		}
	} else if rejectedPower.Cmp(allPower) >= 0 {
		// To avoid repeated commit, let's recheck with count of voters - voter
		if voter == nil || rejectedPower.Sub(rejectedPower, voterPower).Cmp(allPower) < 0 {
			return "rejected"
		}
	}
	return "not determined"
}

type ProposalReactor struct {
	ProposalId  string
	BlockHeight uint64
	Result      string
}

func (pr ProposalReactor) React(result, msg string) {
	now := utils.GetNow()
	if result == "success" {
		// If the default result is not set, then do nothing
		if pr.Result == "" {
			return
		}
		result = pr.Result
	}
	UpdateProposalResult(pr.ProposalId, result, msg, pr.BlockHeight, now)
}

// get the sender from the ctx and ensure it matches the tx pubkey
func getTxSender(ctx types.Context) (sender common.Address, err error) {
	senders := ctx.GetSigners()
	if len(senders) != 1 {
		return sender, ErrMissingSignature()
	}
	return senders[0], nil
}
