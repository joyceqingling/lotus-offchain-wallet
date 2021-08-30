package full

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"github.com/filecoin-project/go-address"
	"github.com/filecoin-project/go-bitfield"
	"github.com/filecoin-project/go-state-types/abi"
	"github.com/filecoin-project/go-state-types/big"
	"github.com/filecoin-project/lotus/api"
	"github.com/filecoin-project/lotus/chain/actors/adt"
	"github.com/filecoin-project/lotus/chain/actors/builtin"
	"github.com/filecoin-project/lotus/chain/actors/builtin/miner"
	"github.com/filecoin-project/lotus/chain/stmgr"
	"github.com/filecoin-project/lotus/chain/types"
	das "github.com/filecoin-project/specs-actors/v5/actors/builtin/miner"
	miner5 "github.com/filecoin-project/specs-actors/v5/actors/builtin/miner"
	"github.com/ipfs/go-cid"
	"github.com/shopspring/decimal"
	cbg "github.com/whyrusleeping/cbor-gen"
	"golang.org/x/xerrors"
	"os/exec"
	"reflect"
	"strconv"
	"strings"
)


func (a *StateAPI) StateMinerDeadlinesByMiner(ctx context.Context, m address.Address, tsk types.TipSetKey) ([]*api.StateMinerDeadlinesByMinerInfo, error) {
	deadlines, err := a.StateMinerDeadlines(ctx, m, tsk)

	r := make([]*api.StateMinerDeadlinesByMinerInfo, 0, len(deadlines))

	if err != nil {
		return []*api.StateMinerDeadlinesByMinerInfo{}, xerrors.Errorf("loading deadlines %s: %w", tsk, err)
	}

	provingDeadline, err := a.StateMinerProvingDeadline(ctx, m, tsk)

	if err != nil {
		return []*api.StateMinerDeadlinesByMinerInfo{}, xerrors.Errorf("loading provingDeadline %s: %w", tsk, err)
	}

	for i, deadline := range deadlines {
		var ll = uint64(i)
		minerDeadlinesInfo  := new(api.StateMinerDeadlinesByMinerInfo)
		partitions, err := a.StateMinerPartitionsNew(ctx, m, ll, tsk)

		if err != nil {
			return []*api.StateMinerDeadlinesByMinerInfo{}, xerrors.Errorf("partitions tipset %s: %w", tsk, err)
		}

		if ll== provingDeadline.Index{
			minerDeadlinesInfo.Index = 1
		}else {
			minerDeadlinesInfo.Index = 0
		}

		var Faults uint64 = 0
		var Recoveries uint64 = 0
		var Sectors uint64 =0
		var Terminated uint64 =0
		var FaultyPower uint64 =0
		var LivePower uint64 =0
		var RecoveringPower uint64 =0


		for _, partition := range partitions {

			RecoveriesCount, _ := partition.Recoveries.Count()
			Recoveries = Recoveries + RecoveriesCount

			FaultsCount, _ := partition.Faults.Count()
			Faults = Faults + FaultsCount

			SectorsCount, _ := partition.Sectors.Count()
			Sectors = Sectors + SectorsCount

			TerminatedCount, _ := partition.Terminated.Count()
			Terminated = Terminated + TerminatedCount

			FaultyPower = FaultyPower + partition.FaultyPower.QA.Uint64()
			LivePower = LivePower + partition.LivePower.QA.Uint64()
			RecoveringPower = RecoveringPower + partition.RecoveringPower.QA.Uint64()


		}
		minerDeadlinesInfo.FaultyPower = FaultyPower
		minerDeadlinesInfo.LivePower = LivePower
		minerDeadlinesInfo.RecoveringPower = RecoveringPower
		minerDeadlinesInfo.Recoveries = Recoveries
		minerDeadlinesInfo.Faults = Faults
		minerDeadlinesInfo.Sectors  = Sectors
		minerDeadlinesInfo.Terminated = Terminated

		minerDeadlinesInfo.PostSubmissions = deadline.PostSubmissions
		minerDeadlinesInfo.Wpost = len(partitions)

		r = append(r, minerDeadlinesInfo)
	}

	return r, nil
}




//func (a *StateAPI ) StateBlockMessages(ctx context.Context,msg cid.Cid) (*api.NBlockMessages, error) {
//	b, err := a.Chain.GetBlock(msg)
//	if err != nil {
//		return nil, err
//	}
//
//
//	bmsgs, smsgs, err := a.NMessagesForBlock(ctx,b)
//	if err != nil {
//		return nil, err
//	}
//
//
//
//	return &api.NBlockMessages{
//		BlsMessages:   bmsgs,
//		SecpkMessages: smsgs,
//	}, nil
//}

func (a *StateAPI) StateMinerPartitionsNew(ctx context.Context, m address.Address, dlIdx uint64, tsk types.TipSetKey) ( []*das.Partition, error) {
	var out  []*das.Partition
	return out, a.StateManager.WithParentStateTsk(tsk,
		a.StateManager.WithActor(m,
			a.StateManager.WithActorState(ctx,
				a.StateManager.WithDeadlines(
					a.StateManager.WithDeadline(dlIdx,
						a.StateManager.WithEachPartition(func(store adt.Store, partIdx uint64, partition  *das.Partition) error {
							out = append(out, partition)
							return nil
						}))))))
}




func (cs *StateAPI) NMessagesForBlock(ctx context.Context,b *types.BlockHeader) ([]*api.NMessage, []*api.NMessage, error) {
	blscids, secpkcids, err := cs.Chain.ReadMsgMetaCids(b.Messages)
	if err != nil {
		return nil, nil, xerrors.Errorf("loading bls messages for block: %w", err)
	}
	blsmsgs, err := cs.NLoadMessagesFromCids(ctx,blscids,b)
	if err != nil {
		return nil, nil, xerrors.Errorf("loading bls messages for block: %w", err)
	}

	secpkmsgs, err := cs.NLoadSignedMessagesFromCids(ctx,secpkcids,b)
	if err != nil {
		return nil, nil, xerrors.Errorf("loading secpk messages for block: %w", err)
	}

	return blsmsgs, secpkmsgs, nil
}


func (cs *StateAPI) NLoadSignedMessagesFromCids(ctx context.Context,cids []cid.Cid,b *types.BlockHeader) ([]*api.NMessage, error) {
	msgs := make([]*api.NMessage, 0, len(cids))
	//nodeAPI, closer, err := GetFullNodeAPI(cs.ctxx)
	//if err != nil {
	//	return nil, xerrors.Errorf("failed to get message: (%s):%d: %w", err)
	//}
	//defer closer()
	//t := types.NewTipSetKey(b.Cid())

	//fmt.Print("MessagesFromCidsByTipSetKey:",t.String())
	for i, c := range cids {
		//var flag =  false
		m, err := cs.Chain.GetSignedMessage(c)
		//fmt.Print("m.To:",m.Message.To)
		//fmt.Print("m.Cid():",m.Cid())
		if err != nil {
			return nil, xerrors.Errorf("failed to get message: (%s):%d: %w", c, i, err)
		}
		nMessage  := new(api.NMessage)
		//actor, err := cs.StateGetActor(ctx, m.Message.To, t)
		//
		//if err != nil {
		//	flag = true
		//}

		//receipt, err := cs.StateGetReceipt(ctx, c, t)
		//
		//if err != nil {
		//	return nil, xerrors.Errorf("failed to get receipt: (%s):%d: %w", c, i, err)
		//}
		//
		//
		//
		//if receipt != nil {
		//	ret, err := jsonReturn(actor.Code, m.Message.Method, receipt.Return)
		//	if err != nil {
		//		return nil, xerrors.Errorf("failed to get jsonReturn: (%s):%d: %w", c, i, err)
		//	}
		//	nMessage.Return = ret
		//	nMessage.ExitCode = receipt.ExitCode
		//	nMessage.GasUsed = receipt.GasUsed
		//}

		//if m.Message.Params != nil {
		//	if !flag {
		//		par, err := jsonParams(actor.Code, m.Message.Method, m.Message.Params)
		//
		//		if err != nil {
		//			return nil, xerrors.Errorf("failed to get jsonParams: (%s):%d: %w", c, i, err)
		//		}
		//		nMessage.Params = par
		//	}
		//}


		nMessage.Version = m.Message.Version
		//nMessage.MessageId = c.String()
		nMessage.To = m.Message.To
		nMessage.From = m.Message.From
		nMessage.Nonce = m.Message.Nonce
		nMessage.Value  = m.Message.Value
		nMessage.GasLimit = m.Message.GasLimit
		nMessage.GasFeeCap = m.Message.GasFeeCap
		nMessage.GasPremium = m.Message.GasPremium
		nMessage.Method = m.Message.Method
		//if !flag {
		//	nMessage.CodeMethod = builtin.ActorNameByCode(actor.Code)
		//
		//}


		msgs = append(msgs, nMessage)
		//msgs = append(msgs, m)
	}

	return msgs, nil
}



func (a *StateAPI) StateGetParentReceipts(ctx context.Context, bcid cid.Cid) ([]*types.MessageReceipt, error) {
	b, err := a.Chain.GetBlock(bcid)
	if err != nil {
		return nil, err
	}


	if b.Height == 0 {
		return nil, nil
	}

	// TODO: need to get the number of messages better than this
	pts, err := a.Chain.LoadTipSet(types.NewTipSetKey(b.Parents...))
	if err != nil {
		return nil, err
	}

	cm,sm, err := a.Chain.MessagesForTipsetEx(pts)
	if err != nil {
		return nil, err
	}

	t := types.NewTipSetKey(bcid)

	var out []*types.MessageReceipt
	for i, msg := range cm {
		r, err := a.Chain.GetParentReceipt(b, i)
		if err != nil {
			return nil, err
		}
		actor, err := a.StateManager.LoadActorTsk(ctx, msg.VMMessage().To, t)

		if err != nil {
			return nil, xerrors.Errorf("failed to get Actor: (%s):%d: %w", msg.VMMessage().To, i, err)
		}

		jsonReturn, err := jsonReturn(actor.Code, msg.VMMessage().Method, r.Return)
		r.Returns = jsonReturn
		//r.BlockCID = msg.VMMessage().BlockCid.String()
		//

		out = append(out, r)
	}


	for i, msg := range sm {
		r, err := a.Chain.GetParentReceipt(b, i)
		if err != nil {
			return nil, err
		}
		actor, err := a.StateManager.LoadActorTsk(ctx, msg.VMMessage().To, t)

		if err != nil {
			return nil, xerrors.Errorf("failed to get Actor: (%s):%d: %w", msg.VMMessage().To, i, err)
		}

		jsonReturn, err := jsonReturn(actor.Code, msg.VMMessage().Method, r.Return)
		r.Returns = jsonReturn
		//r.BlockCID = msg.VMMessage().BlockCid.String()
		//

		out = append(out, r)
	}
	return out, nil
}



func (a *StateAPI) StateReplayList(ctx context.Context, tsp *types.TipSet) ([]*api.InvocResult, error) {

	t, err := stmgr.ComputeStateList(ctx, a.StateManager, tsp)
	if err != nil {
		return nil, err
	}
	return t, nil

}

func (a *StateAPI) StateReplayLists(ctx context.Context, height int64) ([]*api.InvocResult, error) {
	byHeight, err := a.Chain.GetTipsetByHeight(ctx, abi.ChainEpoch(height), nil, true)

	t, err := stmgr.ComputeStateList(ctx, a.StateManager, byHeight)
	if err != nil {
		return nil, err
	}
	return t, nil

}


func (a *StateAPI) StateGetMessageByHeightIntervalEx(ctx context.Context, params api.StateGetMessageByHeightIntervalParams) ([]*api.SMessage, error) {
	marshal, _ := json.Marshal(params)
	log.Info("params ",string(marshal))
	log.Info("params.FromAddress ",params.FromAddress)
	log.Info("params.Method ",params.Method)
	log.Info("params.CodeMethod ",params.CodeMethod)
	log.Info("params.ToAddress ",params.ToAddress)

	blockStart, _ := a.Chain.GetBlock(params.BlockIdStart)
	//blockEnd, _ := a.Chain.GetBlock(params.BlockIdEnd)
	var msgs []*api.SMessage
	//var whereMsgs []*api.SMessage
	for  {
		parents := blockStart.Parents

		//if blockStart.Height < blockEnd.Height {
		//	log.Error("stop for")
		//    break
		//}
		flag := false

		for _, parent := range parents {
			if parent.String() == params.BlockIdEnd.String(){
				flag = true
			}
		}

		if flag{
			log.Error("stop for")
			break
		}


		pts, err := a.Chain.LoadTipSet(types.NewTipSetKey(parents...))
		if err != nil {
			log.Error("loading LoadTipSet %s: %w", pts, err)
			continue
		}


		parentBlockHeader, _ := a.Chain.GetBlock(parents[0])
		cm, err := a.Chain.MessagesForTipset(pts)
		if err != nil {
			log.Error("loading MessagesForTipset %s: %w", pts, err)
			continue
		}


		//t := types.NewTipSetKey(parents[0])
		t := types.EmptyTSK

		for i, msg := range cm {

			r, err := a.Chain.GetParentReceipt(blockStart, i)
			if err != nil {
				log.Error("a.Chain.GetParentReceipt %s: %w", err)
				continue
			}
			var actor  *types.Actor
			actor, err = a.StateManager.LoadActorTsk(ctx, msg.VMMessage().To, t)
			if err != nil {
				actor, err = a.StateGetActor(ctx, msg.VMMessage().To, t)

			}
			a2 := new(api.SMessage)
			a2.Tipset = parents
			a2.Version = msg.VMMessage().Version
			a2.To = msg.VMMessage().To
			a2.From = msg.VMMessage().From
			a2	.Nonce = msg.VMMessage().Nonce
			a2.Value = msg.VMMessage().Value
			a2.GasLimit = msg.VMMessage().GasLimit
			a2.GasFeeCap = msg.VMMessage().GasFeeCap
			a2.GasPremium = msg.VMMessage().GasPremium
			a2.Method = msg.VMMessage().Method
			a2.CID = msg.Cid()
			a2.ExitCode = r.ExitCode
			if actor!=nil {
				a2.CodeMethod = builtin.ActorNameByCode(actor.Code)
				if a2.CodeMethod == "<undefined>" || a2.CodeMethod == "<unknown>"{
					a2.CodeMethod = BuiltinName4(actor.Code)
				}

				if a2.CodeMethod == "<undefined>" || a2.CodeMethod == "<unknown>"{
					a2.CodeMethod = BuiltinName3(actor.Code)
				}
				if a2.CodeMethod == "<undefined>" || a2.CodeMethod == "<unknown>"{
					a2.CodeMethod = BuiltinName2(actor.Code)
				}
				if a2.CodeMethod == "<undefined>" || a2.CodeMethod == "<unknown>"{
					a2.CodeMethod = BuiltinName1(actor.Code)
				}
				if a2.CodeMethod !=""{


					split := strings.Split( a2.CodeMethod, "/")

					bame := split[len(split)-1]

					a2.CodeMethod = bame
				}

				 


			}


			if msg.VMMessage().Params != nil {
				if !flag {
					par, err := jsonParams(actor.Code, msg.VMMessage().Method, msg.VMMessage().Params)

					if err != nil {
						return nil, xerrors.Errorf("failed to get jsonParams: (%s):%d: %w", i, err)
					}
					a2.Params = par
				}
			}

			a2.Height = parentBlockHeader.Height
			msgs = append(msgs, a2)
		}
		blockStart = parentBlockHeader
	}

	var whereMsgs []*api.SMessage
	//筛选

	if  params.CodeMethod !=""&&len(params.Method) >=1 {

		for _, msg := range msgs {
			if params.CodeMethod == msg.CodeMethod{

				for _, method := range params.Method {
					if method == msg.Method.String(){
						whereMsgs = append(whereMsgs,msg )
					}

				}
			}

		}

	}

	if params.CodeMethod !=""&&len(params.Method) >=1&&len(params.ToAddress) >=1{
		for _, msg := range msgs {
			if params.CodeMethod == msg.CodeMethod{

				for _, toAddress := range params.ToAddress {

					for _, method := range params.Method {
						if toAddress == msg.To.String() && method == msg.Method.String(){
							whereMsgs = append(whereMsgs,msg )
						}

					}



				}



			}

		}
	}


	if params.CodeMethod !=""&&len(params.Method) >=1 &&len(params.FromAddress) >=1{
		for _, msg := range msgs {
			if params.CodeMethod == msg.CodeMethod{

				for _, FromAddress := range params.FromAddress {

					for _, method := range params.Method {
						if FromAddress == msg.From.String() && method == msg.Method.String(){
							whereMsgs = append(whereMsgs,msg )
						}
					}

				}




			}
		}
	}





	if params.CodeMethod !="" ||len(params.Method) >=1  ||len(params.ToAddress) >=1 || len(params.FromAddress) >=1{
		return whereMsgs,nil
	}




	return msgs,nil


}


func (a *StateAPI) StateGetParentDeals(ctx context.Context, bcid cid.Cid) ([]*api.StateGetParentDealsResp, error) {
	b, err := a.Chain.GetBlock(bcid)
	if err != nil {
		return nil, err
	}

	if b.Height == 0 {
		return nil, nil
	}

	// TODO: need to get the number of messages better than this
	pts, err := a.Chain.LoadTipSet(types.NewTipSetKey(b.Parents...))
	if err != nil {
		return nil, err
	}

	cm, err := a.Chain.MessagesForTipset(pts)
	if err != nil {
		return nil, err
	}

	t := types.NewTipSetKey(bcid)
	var out []*api.StateGetParentDealsResp
	for i, msg := range cm {
		r, err := a.Chain.GetParentReceipt(b, i)
		if err != nil {
			return nil, err
		}
		var actor  *types.Actor
		actor, err = a.StateManager.LoadActorTsk(ctx, msg.VMMessage().To, t)
		if err != nil {
			actor, err = a.StateGetActor(ctx, msg.VMMessage().To, t)

		}




		if r.Return != nil {
			jsonReturn, err := jsonReturn(actor.Code, msg.VMMessage().Method, r.Return)
			if err != nil {
				return nil, err
			}
			r.Returns = jsonReturn
			code := builtin.ActorNameByCode(actor.Code)

			if code == "<undefined>" || code == "<unknown>"{
				code = BuiltinName4(actor.Code)
			}
			if code == "<undefined>" || code == "<unknown>"{
				code = BuiltinName3(actor.Code)
			}
			if code == "<undefined>" || code == "<unknown>"{
				code = BuiltinName2(actor.Code)
			}
			if code == "<undefined>" || code == "<unknown>"{
				code = BuiltinName1(actor.Code)
			}
			if code !=""{


				split := strings.Split( code, "/")

				bame := split[len(split)-1]

				code = bame
			}


			if code == "storagemarket" && msg.VMMessage().Method == 4  {
				resps := Deals{}
				err := json.Unmarshal([]byte(jsonReturn), &resps)

				if err != nil {
					return nil, xerrors.Errorf("failed to get Actor: (%s):%d: %w", actor.Code, i, err)
				}

				for _, d := range resps.IDs {
					deal, err := a.StateMarketStorageDeal(ctx, d, t)
					if err != nil {
						return nil, xerrors.Errorf("failed to get Actor: (%s):%d: %w", d, i, err)
					}
					key, err := a.StateAccountKey(ctx, deal.Proposal.Client, t)

					if err != nil {
						return nil, xerrors.Errorf("failed to get Actor: (%s):%d: %w", d, i, err)
					}

					a2 := new(api.StateGetParentDealsResp)
					a2.Height = b.Height
					a2.GasUsed = r.GasUsed
					a2.ExitCode = r.ExitCode
					a2.State = deal.State
					a2.Proposal = deal.Proposal
					a2.DealID = d
					a2.CID = msg.VMMessage().Cid()
					a2.ClientWallet = key.String()
					out = append(out, a2)
				}




			}
		}


	}

	return out, nil
}


type Deals struct {
	IDs []abi.DealID
}


func (cs *StateAPI) NLoadMessagesFromCids(ctx context.Context,cids []cid.Cid,b *types.BlockHeader) ([]*api.NMessage, error) {
	msgs := make([]*api.NMessage, 0, len(cids))
	//nodeAPI, closer, err := GetFullNodeAPI(cs.ctxx)
	//if err != nil {
	//	return nil, xerrors.Errorf("failed to get message: (%s):%d: %w", err)
	//}
	//defer closer()
	t := types.NewTipSetKey(b.Cid())
	//fmt.Print("MessagesFromCidsByTipSetKey:",t.String())
	for i, c := range cids {
		var flag = false
		m, err := cs.Chain.GetMessage(c)
		//fmt.Print("m.To:",m.To)
		//fmt.Print("m.Cid():",m.Cid())
		if err != nil {
			return nil, xerrors.Errorf("failed to get message: (%s):%d: %w", c, i, err)
		}
		actor, err := cs.StateManager.LoadActorTsk(ctx, m.To, t)
		//fmt.Print("actorss:",actor)
		//fmt.Print("actorss:",actor.Code.String())
		if err != nil {
			flag = true
		}

		//receipt, err := cs.StateGetReceipt(ctx, c, t)
		//
		//if err != nil {
		//	return nil, xerrors.Errorf("failed to get receipt: (%s):%d: %w", c, i, err)
		//}
		//fmt.Print("actor.Code:",actor.Code)
		//fmt.Print("m.Method:",m.Method)

		nMessage  := new(api.NMessage)
		//if receipt != nil {
		//	//fmt.Print("receipt.Return:",receipt)
		//	ret, err := jsonReturn(actor.Code, m.Method, receipt.Return)
		//	if err != nil {
		//		return nil, xerrors.Errorf("failed to get jsonReturn: (%s):%d: %w", c, i, err)
		//	}
		//	nMessage.Return = ret
		//	nMessage.ExitCode = receipt.ExitCode
		//	nMessage.GasUsed = receipt.GasUsed
		//}

		if m.Params != nil {
			if !flag {
				par, err := jsonParams(actor.Code, m.Method, m.Params)

				if err != nil {
					return nil, xerrors.Errorf("failed to get jsonParams: (%s):%d: %w", c, i, err)
				}
				nMessage.Params = par
			}
		}



		nMessage.Version = m.Version
		//nMessage.MessageId = c.String()
		nMessage.To = m.To
		nMessage.From = m.From
		nMessage.Nonce = m.Nonce
		nMessage.Value  = m.Value
		nMessage.GasLimit = m.GasLimit
		nMessage.GasFeeCap = m.GasFeeCap
		nMessage.GasPremium = m.GasPremium
		nMessage.Method = m.Method
		if !flag {
			//if actor.Code.String() == "bafkqaetgnfwc6mjpon2g64tbm5sw22lomvza"{
			//	nMessage.CodeMethod = "fil/2/storageminer"
			//	//fmt.Print("CodeMethods:",nMessage.CodeMethod)
			//}else {
			nMessage.CodeMethod = builtin.ActorNameByCode(actor.Code)
			//fmt.Print("CodeMethods:",nMessage.CodeMethod)
			//}
		}





		msgs = append(msgs, nMessage)
	}

	return msgs, nil
}


func jsonParams(code cid.Cid, method abi.MethodNum, params []byte) (string, error) {
	p, err := stmgr.GetParamType(code, method)
	if err != nil {
		return "", err
	}

	if err := p.UnmarshalCBOR(bytes.NewReader(params)); err != nil {
		return "", err
	}

	if method == miner.Methods.ProveCommitAggregate {
		var retval miner5.ProveCommitAggregateParams
		if err := retval.UnmarshalCBOR(bytes.NewReader(params)); err != nil {
			return "", err
		}
		sids,err:=retval.SectorNumbers.All(abi.MaxSectorNumber)
		if err != nil {
			return "", err
		}
		var m =map[string]interface{}{}
		m["SectorNumbers"]=sids
		m["AggregateProof"]=retval.AggregateProof

		indent, err := json.MarshalIndent(m, "", "  ")

		return string(indent), err
	}

	if method == miner.Methods.ExtendSectorExpiration {
		var expirationExtensionEx []ExpirationExtensionEx

		var retval miner5.ExtendSectorExpirationParams
		if err := retval.UnmarshalCBOR(bytes.NewReader(params)); err != nil {
			return "", err
		}
		for _, extension := range retval.Extensions {
			all, err := extension.Sectors.All(abi.MaxSectorNumber)
			if err != nil {
				return "", err
			}
			exp := ExpirationExtensionEx{
				Deadline: extension.Deadline,
				Partition: extension.Partition,
				Sectors: all,
				NewExpiration: extension.NewExpiration,
			}
			expirationExtensionEx = append(expirationExtensionEx,exp )
		}

		indent, err := json.MarshalIndent(expirationExtensionEx, "", "  ")

		return string(indent), err
	}

	b, err := json.MarshalIndent(p, "", "  ")
	return string(b), err
}

type ExpirationExtensionEx struct {
	Deadline      uint64
	Partition     uint64
	Sectors       [] uint64
	NewExpiration abi.ChainEpoch
}

func jsonReturn(code cid.Cid, method abi.MethodNum, ret []byte) (string, error) {
	methodMeta, found := stmgr.MethodsMap[code][method]
	if !found {
		return "", fmt.Errorf("method %d not found on actor %s", method, code)
	}
	re := reflect.New(methodMeta.Ret.Elem())
	p := re.Interface().(cbg.CBORUnmarshaler)
	if err := p.UnmarshalCBOR(bytes.NewReader(ret)); err != nil {
		return "", err
	}

	b, err := json.MarshalIndent(p, "", "  ")
	return string(b), err
}

//func (a *StateAPI) StateWithdrawal(ctx context.Context, params *api.StateWithDrawalReq) (string, error) {
//	fmt.Print("StateWithdrawal:",params)
//	//str :=  params.Root+" "+params.Miner+" "+params.EpochPrice+" "+params.MinBlocksDuration
//	//fmt.Print("ClientStartDealGoReq:"+str)
//	cmd := exec.Command("./lotus", "send", params.From ,params.Amount)
//	output,err := cmd.Output()
//	if err != nil {
//		return "", xerrors.Errorf("failed lotus send: %w", err)
//	}
//	s := string(output)
//
//	return  s,nil
//
//
//}



func (a *StateAPI) StateBlockMessages(ctx context.Context, msg cid.Cid) (*api.StateBlockMessagesRes, error) {
	b, err := a.Chain.GetBlock(msg)

	if err != nil {
		return nil, err
	}

	bmsgs, smsgs, err := a.MessagesForBlock(b,ctx)
	if err != nil {
		return nil, err
	}

	//cids := make([]cid.Cid, len(bmsgs)+len(smsgs))
	//
	//for i, m := range bmsgs {
	//
	//	cids[i] = m.Cid()
	//}
	//
	//for i, m := range smsgs {
	//	cids[i+len(bmsgs)] = m.Cid()
	//}

	return &api.StateBlockMessagesRes{
		BlsMessages:   bmsgs,
		SecpkMessages: smsgs,
	}, nil
}


func (cs *StateAPI) MessagesForBlock(b *types.BlockHeader,ctx context.Context) ([]*api.NMessage, []*api.NMessage, error) {
	blscids, secpkcids, err := cs.Chain.ReadMsgMetaCids(b.Messages)
	if err != nil {
		return nil, nil, err
	}

	blsmsgs, err := cs.LoadMessagesFromCids(blscids,b,ctx)
	if err != nil {
		return nil, nil, xerrors.Errorf("loading bls messages for block: %w", err)
	}

	secpkmsgs, err := cs.LoadSignedMessagesFromCids(secpkcids,b,ctx)
	if err != nil {
		return nil, nil, xerrors.Errorf("loading secpk messages for block: %w", err)
	}

	return blsmsgs, secpkmsgs, nil
}


func (cs *StateAPI) LoadMessagesFromCids(cids []cid.Cid,b *types.BlockHeader,ctx context.Context) ([]*api.NMessage, error) {

	msgs := make([]*api.NMessage, 0, len(cids))

	t := types.NewTipSetKey(b.Cid())


	for i, c := range cids {
		var flag = false
		m, err := cs.Chain.GetMessage(c)


		if err != nil {
			return nil, xerrors.Errorf("failed to get message: (%s):%d: %w", c, i, err)
		}
		var actor  *types.Actor
		actor, err = cs.StateManager.LoadActorTsk(ctx, m.To, t)
		if err != nil {
			actor, err = cs.StateGetActor(ctx, m.To, t)
			if(err != nil){
				flag = true
			}
		}


		call, err := cs.StateCall(ctx, m,t)
		if err != nil {
			xerrors.Errorf("failed to get message: (%s):%d: %w", c, i, err)
		}



		nMessage  := new(api.NMessage)

		if m.Params != nil {
			if !flag {
				par, err := jsonParams(actor.Code, m.Method, m.Params)

				if err != nil {
					return nil, xerrors.Errorf("failed to get jsonParams: (%s):%d: %w", c, i, err)
				}
				nMessage.Params = par
			}
		}


		if call != nil {
			if call.MsgRct.Return !=nil {
				if !flag {
					returns, err := jsonReturn(actor.Code, m.Method, call.MsgRct.Return)
					if err != nil {
						return nil, xerrors.Errorf("failed to get jsonParams: (%s):%d: %w", c, i, err)
					}
					call.MsgRct.Returns = returns

				}

			}

			nMessage.MsgRct = call.MsgRct

		}


		nMessage.Version = m.Version
		nMessage.To = m.To
		nMessage.From = m.From
		nMessage.Nonce = m.Nonce
		nMessage.Value  = m.Value
		nMessage.GasLimit = m.GasLimit
		nMessage.GasFeeCap = m.GasFeeCap
		nMessage.GasPremium = m.GasPremium
		nMessage.Method = m.Method
		nMessage.CID = c
		nMessage.GasCost = call.GasCost
		nMessage.ExecutionTrace = call.ExecutionTrace
		nMessage.Duration = call.Duration
		nMessage.Error = call.Error
		if !flag {
			nMessage.CodeMethod = builtin.ActorNameByCode(actor.Code)
		}
		msgs = append(msgs, nMessage)
	}

	return msgs, nil
}


func (cs *StateAPI) LoadSignedMessagesFromCids(cids []cid.Cid,b *types.BlockHeader,ctx context.Context) ([]*api.NMessage, error) {
	msgs := make([]*api.NMessage, 0, len(cids))
	t := types.NewTipSetKey(b.Cid())
	for i, c := range cids {
		var flag = false
		m, err := cs.Chain.GetSignedMessage(c)
		if err != nil {
			return nil, xerrors.Errorf("failed to get message: (%s):%d: %w", c, i, err)
		}

		var actor  *types.Actor
		actor, err = cs.StateManager.LoadActorTsk(ctx, m.VMMessage().To, t)
		if err != nil {
			actor, err = cs.StateGetActor(ctx, m.VMMessage().To, t)
			if(err != nil){
				flag = true
			}
		}


		call, err := cs.StateCall(ctx, &m.Message,t)
		if err != nil {
			xerrors.Errorf("failed to get message: (%s):%d: %w", c, i, err)
		}



		nMessage  := new(api.NMessage)


		if m.VMMessage().Params != nil {
			if !flag {
				par, err := jsonParams(actor.Code, m.VMMessage().Method, m.VMMessage().Params)

				if err != nil {
					return nil, xerrors.Errorf("failed to get jsonParams: (%s):%d: %w", c, i, err)
				}
				nMessage.Params = par
			}
		}

		if call != nil {
			if call.MsgRct.Return !=nil {
				if !flag {
					returns, err := jsonReturn(actor.Code, m.VMMessage().Method, call.MsgRct.Return)
					if err != nil {
						return nil, xerrors.Errorf("failed to get jsonParams: (%s):%d: %w", c, i, err)
					}
					call.MsgRct.Returns = returns

				}

			}

			nMessage.MsgRct = call.MsgRct

		}

		nMessage.Version = m.VMMessage().Version
		nMessage.To = m.VMMessage().To
		nMessage.From = m.VMMessage().From
		nMessage.Nonce = m.VMMessage().Nonce
		nMessage.Value  = m.VMMessage().Value
		nMessage.GasLimit = m.VMMessage().GasLimit
		nMessage.GasFeeCap = m.VMMessage().GasFeeCap
		nMessage.GasPremium = m.VMMessage().GasPremium
		nMessage.Method = m.VMMessage().Method
		nMessage.CID = c
		nMessage.GasCost = call.GasCost
		nMessage.ExecutionTrace = call.ExecutionTrace
		nMessage.Duration = call.Duration
		nMessage.Error = call.Error
		if !flag {
			nMessage.CodeMethod = builtin.ActorNameByCode(actor.Code)

		}

		msgs = append(msgs, nMessage)
	}

	return msgs, nil
}


func (a *StateAPI) StateCallExt(ctx context.Context, msg *types.Message,priorMsgs types.ChainMsg, tsk types.TipSetKey) (res *api.InvocResult, err error) {
	ts, err := a.Chain.GetTipSetFromKey(tsk)
	if err != nil {
		return nil, xerrors.Errorf("loading tipset %s: %w", tsk, err)
	}
	for {
		res, err = a.StateManager.CallExt(ctx, msg,ts)
		if err != stmgr.ErrExpensiveFork {
			break
		}
		ts, err = a.Chain.GetTipSetFromKey(ts.Parents())
		if err != nil {
			return nil, xerrors.Errorf("getting parent tipset: %w", err)
		}
	}
	return res, err
}



func (a *StateAPI) StateReplayTest(ctx context.Context, tsk types.TipSetKey, mc []cid.Cid) ([]*api.InvocResult, error) {
	var rets []*api.InvocResult


	return  rets,nil
}



func (a *StateAPI) StateReplayEx(ctx context.Context, tsk types.TipSetKey) ([]*api.InvocResults, error) {

	//var ts *types.TipSet
	//var err error
	//
	//ts, err = a.Chain.LoadTipSet(tsk)
	//if err != nil {
	//	return nil, xerrors.Errorf("loading specified tipset %s: %w", tsk, err)
	//}

	//_, r, err := a.StateManager.ReplayEx(ctx, ts)

	return  nil,nil

}



func (a *StateAPI) StateComputeBaseFee(ctx context.Context, bcid cid.Cid) (abi.TokenAmount, error) {
	b, err := a.Chain.GetBlock(bcid)
	if err != nil {
		return abi.TokenAmount{}, xerrors.Errorf("loading tipset %s: %w", b, err)
	}

	tsk := types.NewTipSetKey(b.Parents...)
	ts, err := a.Chain.LoadTipSet(tsk)


	if err != nil {
		return abi.TokenAmount{}, xerrors.Errorf("loading tipset %s: %w", tsk, err)
	}
	fee, err := a.StateManager.ChainStore().ComputeBaseFee(ctx, ts)

	if err != nil {
		return abi.TokenAmount{}, xerrors.Errorf("loading StateComputeBaseFee %s: %w", tsk, err)
	}
	return  fee,nil
}


func (a *StateAPI) StateComputeBaseFeeTest(ctx context.Context) (abi.TokenAmount, error) {

	return  abi.NewTokenAmount(100),nil
}

func (a *StateAPI) StateGetMessageByHeightInterval(ctx context.Context, height int64) ([]*api.NMessage, error) {


	var out []*api.NMessage


	byHeight, err := a.Chain.GetTipsetByHeight(ctx, abi.ChainEpoch(height), nil, true)

	if byHeight == nil {
		return nil, xerrors.Errorf("loading GetTipsetByHeight %s: %w", byHeight, err)

	}
	log.Info("byHeight",byHeight.Height(),abi.ChainEpoch(height))
	if  byHeight.Height().String()!=abi.ChainEpoch(height).String(){
		return nil, xerrors.Errorf("loading GetTipsetByHeight %s: %w", byHeight, err)
	}

	set := a.Chain.GetHeaviestTipSet()
	cids := set.Cids()
	c := cids[0]
	headers, err := a.Chain.GetBlock(c)
	if err != nil {
		return nil, err
	}


	if err != nil {
		return nil, xerrors.Errorf("loading GetTipsetByHeight %s: %w", byHeight, err)
	}

	list, err := a.StateReplayList(ctx,byHeight)
	for _, header := range byHeight.Blocks() {

		b, err := a.Chain.GetBlock(header.Cid())
		if err != nil {
			return nil, err
		}

		blscids, secpkcids, err := a.Chain.ReadMsgMetaCids(b.Messages)
		if err != nil {
			return nil, err
		}

		out, err = a.LoadMessagesFromCidsByHeight(blscids, headers, ctx, out, list,header)

		if err != nil {
			return nil, xerrors.Errorf("loading LoadMessagesFromCidsByHeight %s: %w", byHeight, err)
		}

		out, err = a.LoadSignedMessagesFromCidsByHeight(secpkcids, headers, ctx, out, list,header)

		if err != nil {
			return nil, xerrors.Errorf("loading fromCidsByHeight %s: %w", byHeight, err)
		}

	}
	return  out,nil


}



func (a *StateAPI) StateGetBlockByHeightInterval(ctx context.Context, height int64) ([]*types.BlockHeaders, error) {


	var out []*types.BlockHeaders


	byHeight, err := a.Chain.GetTipsetByHeight(ctx, abi.ChainEpoch(height), nil, true)

	if byHeight == nil {
		return nil, xerrors.Errorf("loading GetTipsetByHeight %s: %w", byHeight, err)

	}
	log.Info("byHeight",byHeight.Height(),abi.ChainEpoch(height))
	if  byHeight.Height().String()!=abi.ChainEpoch(height).String(){
		return nil, xerrors.Errorf("loading GetTipsetByHeight %s: %w", byHeight, err)
	}




	if err != nil {
		return nil, xerrors.Errorf("loading GetTipsetByHeight %s: %w", byHeight, err)
	}

	for _, header := range byHeight.Blocks() {

		reward, err := a.StateGetRewardLastPerEpochReward(ctx, types.NewTipSetKey(header.Cid()))

		if err != nil {
			return nil, xerrors.Errorf("loading GetTipsetByHeight %s: %w", byHeight, err)
		}

		t := new(types.BlockHeaders)

		t.BlockId = header.Cid()
		t.Miner = header.Miner
		t.Ticket = header.Ticket
		t.ElectionProof = header.ElectionProof
		t.BeaconEntries = header.BeaconEntries
		t.WinPoStProof = header.WinPoStProof
		t.Parents = header.Parents
		t.ParentWeight = header.ParentWeight
		t.Height = header.Height
		t.ParentStateRoot = header.ParentStateRoot
		t.ParentMessageReceipts = header.ParentMessageReceipts
		t.Messages = header.Messages
		t.BLSAggregate = header.BLSAggregate
		t.Timestamp = header.Timestamp
		t.BlockSig = header.BlockSig
		t.ForkSignaling = header.ForkSignaling
		t.ParentBaseFee = header.ParentBaseFee
		t.Reward = reward
		out =  append(out, t);

	}
	return  out,nil


}


func (a *StateAPI) StateGetBlockByHeightWhere(ctx context.Context, params api.StateGetBlockByHeightIntervalParams) ([]*types.BlockHeaders, error) {

	startHeight := params.StartHeight
	endHeight := params.EndHeight

	heightfix := endHeight-startHeight

	height := startHeight-1

	var out []*types.BlockHeaders
	for i := 0; i <= heightfix; i++ {
		if height >endHeight{
			break
		}
		height = height+1



		byHeight, err := a.Chain.GetTipsetByHeight(ctx, abi.ChainEpoch(height), nil, true)

		if byHeight == nil {
			continue;

		}
		log.Info("byHeight",byHeight.Height(),abi.ChainEpoch(height))
		if  byHeight.Height().String()!=abi.ChainEpoch(height).String(){
			continue;
			//return nil, xerrors.Errorf("loading GetTipsetByHeight %s: %w", byHeight, err)
		}




		if err != nil {
			continue;
		}

		for _, header := range byHeight.Blocks() {

			reward, err := a.StateGetRewardLastPerEpochReward(ctx, types.NewTipSetKey(header.Cid()))

			if err != nil {
				return nil, xerrors.Errorf("loading GetTipsetByHeight %s: %w", byHeight, err)
			}

			t := new(types.BlockHeaders)

			t.BlockId = header.Cid()
			t.Miner = header.Miner
			t.Ticket = header.Ticket
			t.ElectionProof = header.ElectionProof
			t.BeaconEntries = header.BeaconEntries
			t.WinPoStProof = header.WinPoStProof
			t.Parents = header.Parents
			t.ParentWeight = header.ParentWeight
			t.Height = header.Height
			t.ParentStateRoot = header.ParentStateRoot
			t.ParentMessageReceipts = header.ParentMessageReceipts
			t.Messages = header.Messages
			t.BLSAggregate = header.BLSAggregate
			t.Timestamp = header.Timestamp
			t.BlockSig = header.BlockSig
			t.ForkSignaling = header.ForkSignaling
			t.ParentBaseFee = header.ParentBaseFee
			t.Reward = reward
			out =  append(out, t);

		}

	}

	var sout []*types.BlockHeaders

	if len(params.MinerAddress) >0 {
		for _, headers := range out {
			for _, minerAddress := range params.MinerAddress {
				if minerAddress == headers.Miner.String() {

					sout =	append(sout, headers)
				}

			}

		}
		return sout,nil
	}




	return  out,nil


}



func (a *StateAPI) StateGetMessageDealsByHeightInterval(ctx context.Context, height int64) ([]*api.StateGetParentDealsResp, error) {
	log.Info("comming  StateGetMessageDealsByHeightInterval")
	height = height +1




	byHeight, err := a.Chain.GetTipsetByHeight(ctx, abi.ChainEpoch(height), nil, true)

	if byHeight == nil {
		return nil, xerrors.Errorf("loading GetTipsetByHeight %s: %w", byHeight, err)

	}



	log.Info("byHeight",byHeight.Height(),abi.ChainEpoch(height))
	if  byHeight.Height().String()!=abi.ChainEpoch(height).String(){
		return nil, xerrors.Errorf("loading GetTipsetByHeight %s: %w", byHeight, err)
	}

	header := byHeight.Blocks()[0]
	deals, err := a.StateGetParentDeals(ctx, header.Cid())


	if err != nil {
		return nil, xerrors.Errorf("loading GetTipsetByHeight %s: %w", byHeight, err)
	}

	return  deals,nil


}



//func (a *StateAPI) StateGetMessageDealsByHeightInterval(ctx context.Context, height int64) ([]*api.StateGetMessageDealsByHeightIntervalResp, error) {
//
//	var resp []*api.StateGetMessageDealsByHeightIntervalResp
//
//
//	var out []*api.NMessage
//
//
//	byHeight, err := a.Chain.GetTipsetByHeight(ctx, abi.ChainEpoch(height), nil, true)
//
//	if byHeight == nil {
//		return nil, xerrors.Errorf("loading GetTipsetByHeight %s: %w", byHeight, err)
//
//	}
//
//
//
//	if err != nil {
//		return nil, xerrors.Errorf("loading GetTipsetByHeight %s: %w", byHeight, err)
//	}
//
//	//list, err := a.StateReplayList(ctx,byHeight)
//	for _, header := range byHeight.Blocks() {
//
//		b, err := a.Chain.GetBlock(header.Cid())
//		if err != nil {
//			return nil, err
//		}
//
//		blscids, secpkcids, err := a.Chain.ReadMsgMetaCids(b.Messages)
//		if err != nil {
//			return nil, err
//		}
//
//		resp, err = a.LoadMessagesFromCidsByHeightDeal(blscids, header, ctx, out,resp)
//
//		if err != nil {
//			return nil, xerrors.Errorf("loading LoadMessagesFromCidsByHeight %s: %w", byHeight, err)
//		}
//
//		resp, err = a.LoadSignedMessagesFromCidsByHeightDeal(secpkcids, header, ctx, out,resp)
//
//		if err != nil {
//			return nil, xerrors.Errorf("loading fromCidsByHeight %s: %w", byHeight, err)
//		}
//
//	}
//	return  resp,nil
//
//
//}



func (cs *StateAPI) LoadMessagesFromCidsByHeight(cids []cid.Cid,b *types.BlockHeader,ctx context.Context,msgs []*api.NMessage,list []*api.InvocResult,block *types.BlockHeader) ([]*api.NMessage, error) {


	//t := types.NewTipSetKey(b.Cid())
	t := types.EmptyTSK

	for i, c := range cids {


		var flag = false
		m, err := cs.Chain.GetMessage(c)


		if err != nil {
			return nil, xerrors.Errorf("failed to get message: (%s):%d: %w", c, i, err)
		}


		var invocResult  *api.InvocResult
		for _, results := range list {
			if results.MsgCid.String() == m.Cid().String(){
				invocResult  = results
				continue
			}


		}


		var actor  *types.Actor
		actor, err = cs.StateManager.LoadActorTsk(ctx, m.To, t)
		if err != nil {
			actor, err = cs.StateGetActor(ctx, m.To, t)
			if(err != nil){
				flag = true
			}else {
				log.Debugf("hhactorCode is null",c,t)
			}
		}






		nMessage  := new(api.NMessage)

		if m.Params != nil {
			if !flag {
				par, err := jsonParams(actor.Code, m.Method, m.Params)

				if err != nil {
					return nil, xerrors.Errorf("failed to get jsonParams: (%s):%d: %w", c, i, err)
				}
				nMessage.Params = par
			}
		}


		if invocResult != nil {
			if invocResult.MsgRct.Return !=nil {
				if !flag {
					returns, err := jsonReturn(actor.Code, m.VMMessage().Method, invocResult.MsgRct.Return)
					if err != nil {
						return nil, xerrors.Errorf("failed to get jsonParams: (%s):%d: %w", err)
					}
					invocResult.MsgRct.Returns = returns

				}

			}

			nMessage.GasCost = invocResult.GasCost
			nMessage.ExecutionTrace = invocResult.ExecutionTrace
			nMessage.Duration = invocResult.Duration
			nMessage.Error = invocResult.Error
			nMessage.MsgRct = invocResult.MsgRct
			nMessage.Timestamp = invocResult.Timestamp
		}else {
			mcp  := new(types.MessageReceipt)
			mcp.ExitCode = -1
			nMessage.MsgRct = mcp
		}

		nMessage.Paramss = m.Params
		nMessage.Version = m.Version
		nMessage.To = m.To
		nMessage.From = m.From
		nMessage.Nonce = m.Nonce
		nMessage.Value  = m.Value
		nMessage.GasLimit = m.GasLimit
		nMessage.GasFeeCap = m.GasFeeCap
		nMessage.GasPremium = m.GasPremium
		nMessage.Method = m.Method
		nMessage.CID = c
		nMessage.BlockCid = block.Cid()
		if !flag {
			nMessage.CodeMethod = builtin.ActorNameByCode(actor.Code)
			if nMessage.CodeMethod == "<undefined>" || nMessage.CodeMethod == "<unknown>"{
				nMessage.CodeMethod = BuiltinName4(actor.Code)
			}

			if nMessage.CodeMethod == "<undefined>" || nMessage.CodeMethod == "<unknown>"{
				nMessage.CodeMethod = BuiltinName3(actor.Code)
			}

			if nMessage.CodeMethod == "<undefined>" || nMessage.CodeMethod == "<unknown>"{
				nMessage.CodeMethod = BuiltinName2(actor.Code)
			}
			if nMessage.CodeMethod == "<undefined>" || nMessage.CodeMethod == "<unknown>"{
				nMessage.CodeMethod = BuiltinName1(actor.Code)
			}
			if nMessage.CodeMethod !=""{


				split := strings.Split( nMessage.CodeMethod, "/")

				bame := split[len(split)-1]

				nMessage.CodeMethod = bame
			}else {
				log.Debugf("codeMethod is null",nMessage.CID,actor.Code)
			}
		}
		msgs = append(msgs, nMessage)
	}

	return msgs, nil
}



func (cs *StateAPI) LoadMessagesFromCidsByHeightDeal(cids []cid.Cid,b *types.BlockHeader,ctx context.Context,msgs []*api.NMessage,deal []*api.StateGetMessageDealsByHeightIntervalResp) ([]*api.StateGetMessageDealsByHeightIntervalResp, error) {


	t := types.NewTipSetKey(b.Cid())


	for i, c := range cids {


		var flag = false
		m, err := cs.Chain.GetMessage(c)


		if err != nil {
			return nil, xerrors.Errorf("failed to get message: (%s):%d: %w", c, i, err)
		}


		//var invocResult  *api.InvocResults
		//for _, results := range list {
		//	if results.MsgCid.String() == m.Cid().String(){
		//		invocResult  = results
		//		continue
		//	}
		//
		//
		//}


		var actor  *types.Actor
		actor, err = cs.StateManager.LoadActorTsk(ctx, m.To, t)
		if err != nil {
			actor, err = cs.StateGetActor(ctx, m.To, t)
			if(err != nil){
				flag = true
			}
		}






		nMessage  := new(api.NMessage)

		if m.Params != nil {
			if !flag {
				par, err := jsonParams(actor.Code, m.Method, m.Params)

				if err != nil {
					return nil, xerrors.Errorf("failed to get jsonParams: (%s):%d: %w", c, i, err)
				}
				nMessage.Params = par
			}
		}


		//if invocResult != nil {
		//	if invocResult.MsgRct.Return !=nil {
		//		if !flag {
		//			returns, err := jsonReturn(actor.Code, m.VMMessage().Method, invocResult.MsgRct.Return)
		//			if err != nil {
		//				return nil, xerrors.Errorf("failed to get jsonParams: (%s):%d: %w", err)
		//			}
		//			invocResult.MsgRct.Returns = returns
		//
		//		}
		//
		//	}
		//
		//	nMessage.GasCost = invocResult.GasCost
		//	nMessage.ExecutionTrace = invocResult.ExecutionTrace
		//	nMessage.Duration = invocResult.Duration
		//	nMessage.Error = invocResult.Error
		//	nMessage.MsgRct = invocResult.MsgRct
		//
		//}else {
		//	mcp  := new(types.MessageReceipt)
		//	mcp.ExitCode = -1
		//	nMessage.MsgRct = mcp
		//}


		nMessage.Version = m.Version
		nMessage.To = m.To
		nMessage.From = m.From
		nMessage.Nonce = m.Nonce
		nMessage.Value  = m.Value
		nMessage.GasLimit = m.GasLimit
		nMessage.GasFeeCap = m.GasFeeCap
		nMessage.GasPremium = m.GasPremium
		nMessage.Method = m.Method
		nMessage.CID = c
		nMessage.BlockCid = b.Cid()
		if !flag {
			nMessage.CodeMethod = builtin.ActorNameByCode(actor.Code)
			if nMessage.CodeMethod == "fil/4/storagemarket" && nMessage.Method == 4{
				tsk := types.EmptyTSK

				a := new(api.StateGetMessageDealsByHeightIntervalResp)
				var marketDeal []*api.MarketDeal
				resps := Deals{}
				err := json.Unmarshal([]byte(nMessage.MsgRct.Returns), &resps)

				if err != nil {
					return nil, xerrors.Errorf("failed to get Actor: (%s):%d: %w", nMessage.MsgRct.Returns, nMessage.CID.String(), err)
				}

				for _, d := range resps.IDs {
					deal, err := cs.StateMarketStorageDeal(ctx, d, tsk)
					if err != nil {
						return nil, xerrors.Errorf("failed to get Actor: (%s):%d: %w", d, i, err)
					}
					key, err := cs.StateAccountKey(ctx, deal.Proposal.Client, tsk)

					if err != nil {
						return nil, xerrors.Errorf("failed to get Actor: (%s):%d: %w", d, i, err)
					}

					deal.Proposal.Client = key

					marketDeal = append(marketDeal,deal )

				}

				a.Msg = nMessage
				a.Deal = marketDeal
				deal =  append(deal, a)

			}

		}
		//msgs = append(msgs, nMessage)
	}

	return deal, nil
}


func (cs *StateAPI) LoadSignedMessagesFromCidsByHeightDeal(cids []cid.Cid,b *types.BlockHeader,ctx context.Context,msgs []*api.NMessage,deal []*api.StateGetMessageDealsByHeightIntervalResp) ([]*api.StateGetMessageDealsByHeightIntervalResp, error) {

	t := types.NewTipSetKey(b.Cid())
	for i, c := range cids {
		var flag = false
		m, err := cs.Chain.GetSignedMessage(c)
		if err != nil {
			return nil, xerrors.Errorf("failed to get message: (%s):%d: %w", c, i, err)
		}



		var actor  *types.Actor
		actor, err = cs.StateManager.LoadActorTsk(ctx, m.VMMessage().To, t)
		if err != nil {
			actor, err = cs.StateGetActor(ctx, m.VMMessage().To, t)
			if(err != nil){
				flag = true
			}
		}





		nMessage  := new(api.NMessage)


		if m.VMMessage().Params != nil {
			if !flag {
				par, err := jsonParams(actor.Code, m.VMMessage().Method, m.VMMessage().Params)

				if err != nil {
					return nil, xerrors.Errorf("failed to get jsonParams: (%s):%d: %w", c, i, err)
				}
				nMessage.Params = par
			}
		}



		nMessage.Version = m.VMMessage().Version
		nMessage.To = m.VMMessage().To
		nMessage.From = m.VMMessage().From
		nMessage.Nonce = m.VMMessage().Nonce
		nMessage.Value  = m.VMMessage().Value
		nMessage.GasLimit = m.VMMessage().GasLimit
		nMessage.GasFeeCap = m.VMMessage().GasFeeCap
		nMessage.GasPremium = m.VMMessage().GasPremium
		nMessage.Method = m.VMMessage().Method
		nMessage.CID = c
		nMessage.BlockCid = b.Cid()
		if !flag {
			nMessage.CodeMethod = builtin.ActorNameByCode(actor.Code)
			if nMessage.CodeMethod == "fil/4/storagemarket" && nMessage.Method == 4{
				tsk := types.EmptyTSK
				a := new(api.StateGetMessageDealsByHeightIntervalResp)
				var marketDeal []*api.MarketDeal
				resps := Deals{}
				err := json.Unmarshal([]byte(nMessage.MsgRct.Returns), &resps)

				if err != nil {
					return nil, xerrors.Errorf("failed to get Actor: (%s):%d: %w", nMessage.MsgRct.Returns, nMessage.CID, err)
				}

				for _, d := range resps.IDs {
					deal, err := cs.StateMarketStorageDeal(ctx, d, tsk)
					if err != nil {
						return nil, xerrors.Errorf("failed to get Actor: (%s):%d: %w", d, i, err)
					}
					key, err := cs.StateAccountKey(ctx, deal.Proposal.Client, tsk)

					if err != nil {
						return nil, xerrors.Errorf("failed to get Actor: (%s):%d: %w", d, i, err)
					}

					deal.Proposal.Client = key

					marketDeal = append(marketDeal,deal )

				}

				a.Msg = nMessage
				a.Deal = marketDeal
				deal =  append(deal, a)

			}

		}

	}

	return deal, nil
}



func (cs *StateAPI) LoadSignedMessagesFromCidsByHeight(cids []cid.Cid,b *types.BlockHeader,ctx context.Context,msgs []*api.NMessage,list []*api.InvocResult,block *types.BlockHeader) ([]*api.NMessage, error) {
	//t := types.NewTipSetKey(b.Cid())
	t := types.EmptyTSK
	for i, c := range cids {
		var flag = false
		m, err := cs.Chain.GetSignedMessage(c)
		if err != nil {
			return nil, xerrors.Errorf("failed to get message: (%s):%d: %w", c, i, err)
		}

		var invocResult  *api.InvocResult
		for _, results := range list {
			if results.MsgCid.String() == m.Cid().String(){
				invocResult  = results
				continue
			}


		}

		var actor  *types.Actor
		actor, err = cs.StateManager.LoadActorTsk(ctx, m.VMMessage().To, t)
		if err != nil {
			actor, err = cs.StateGetActor(ctx, m.VMMessage().To, t)
			if(err != nil){
				flag = true
			}else {
				log.Debugf("hhactorCode is null",c,t)
			}
		}





		nMessage  := new(api.NMessage)


		if m.VMMessage().Params != nil {
			if !flag {
				par, err := jsonParams(actor.Code, m.VMMessage().Method, m.VMMessage().Params)

				if err != nil {

					//if err != nil {
					//	return nil, xerrors.Errorf("failed to get jsonParams: (%s):%d: %w", err)
					//}
					//log.Errorf("failed to get jsonParams: (%s):%d: %w", c, i, err)
					nMessage.Params = par
				}

			}
		}

		if invocResult != nil {
			if invocResult.MsgRct.Return !=nil {
				if !flag {
					returns, err := jsonReturn(actor.Code, m.VMMessage().Method, invocResult.MsgRct.Return)
					if err != nil {
						return nil, xerrors.Errorf("failed to get jsonParams: (%s):%d: %w", err)
					}
					invocResult.MsgRct.Returns = returns

				}

			}

			nMessage.GasCost = invocResult.GasCost
			nMessage.ExecutionTrace = invocResult.ExecutionTrace
			nMessage.Duration = invocResult.Duration
			nMessage.Error = invocResult.Error
			nMessage.MsgRct = invocResult.MsgRct
			nMessage.Timestamp = invocResult.Timestamp
		}else {
			mcp  := new(types.MessageReceipt)
			mcp.ExitCode = -1
			nMessage.MsgRct = mcp
		}

		nMessage.Paramss = m.VMMessage().Params
		nMessage.Version = m.VMMessage().Version
		nMessage.To = m.VMMessage().To
		nMessage.From = m.VMMessage().From
		nMessage.Nonce = m.VMMessage().Nonce
		nMessage.Value  = m.VMMessage().Value
		nMessage.GasLimit = m.VMMessage().GasLimit
		nMessage.GasFeeCap = m.VMMessage().GasFeeCap
		nMessage.GasPremium = m.VMMessage().GasPremium
		nMessage.Method = m.VMMessage().Method
		nMessage.CID = c
		nMessage.BlockCid = block.Cid()
		if !flag {
			nMessage.CodeMethod = builtin.ActorNameByCode(actor.Code)
			if nMessage.CodeMethod == "<undefined>" || nMessage.CodeMethod == "<unknown>"{
				nMessage.CodeMethod = BuiltinName4(actor.Code)
			}
			if nMessage.CodeMethod == "<undefined>" || nMessage.CodeMethod == "<unknown>"{
				nMessage.CodeMethod = BuiltinName3(actor.Code)
			}

			if nMessage.CodeMethod == "<undefined>" || nMessage.CodeMethod == "<unknown>"{
				nMessage.CodeMethod = BuiltinName2(actor.Code)
			}
			if nMessage.CodeMethod == "<undefined>" || nMessage.CodeMethod == "<unknown>"{
				nMessage.CodeMethod = BuiltinName1(actor.Code)
			}
			if nMessage.CodeMethod !=""{


				split := strings.Split( nMessage.CodeMethod, "/")

				bame := split[len(split)-1]

				nMessage.CodeMethod = bame
			}else {
				log.Debugf("codeMethod is null",nMessage.CID)
			}

		}

		msgs = append(msgs, nMessage)
	}

	return msgs, nil
}







func (a *StateAPI) StateGetBlockByHeight(ctx context.Context, start int64, end int64) ([]*api.Blocks, error) {

	ts, err := a.Chain.GetTipSetFromKey(types.EmptyTSK)
	if err != nil {
		return nil, xerrors.Errorf("loading tipset %s: %w", err)
	}

	startTs, err := a.Chain.GetTipsetByHeight(ctx, abi.ChainEpoch(start), ts, true)
	if err != nil {
		return nil, xerrors.Errorf("loading tipset %s: %w", err)
	}
	var resp []*api.Blocks
	if(end==2){


		for _, header := range startTs.Blocks() {
			a2 := new(api.Blocks)

			reward, err := a.StateGetRewardLastPerEpochReward(ctx, types.NewTipSetKey(header.Cid()))

			if err != nil {
				return nil, xerrors.Errorf("loading reward %s: %w",reward, err)
			}
			a2.Cid = header.Cid().String()
			a2.Miner = header.Miner
			a2.Reward = reward
			a2.Height = header.Height
			a2.Timestamp = header.Timestamp
			resp = append(resp, a2)
		}

		return resp,nil

	}

	set := a.Chain.GetHeaviestTipSet()



	i1, err := strconv.ParseInt(set.Height().String(), 10, 64)


	if err != nil {
		return nil, xerrors.Errorf("loading reward %s: %w", err)
	}

	num := i1 - start

	strInt64 := strconv.FormatInt(num, 10)
	id16 ,_ := strconv.Atoi(strInt64)

	startHeight := start-1
	for i := 0; i <= id16; i++ {
		startHeight = startHeight+1
		log.Info("startHeight",startHeight)

		tss, err := a.Chain.GetTipsetByHeight(ctx, abi.ChainEpoch(startHeight), ts, true)
		if err != nil {
			return nil, xerrors.Errorf("loading reward %s: %w", err)
		}
		for _, header := range tss.Blocks() {

			a2 := new(api.Blocks)

			reward, err := a.StateGetRewardLastPerEpochReward(ctx, types.NewTipSetKey(header.Cid()))

			if err != nil {
				return nil, xerrors.Errorf("loading reward %s: %w",reward, err)
			}
			a2.Cid = header.Cid().String()
			a2.Miner = header.Miner
			a2.Reward = reward
			a2.Height = header.Height
			a2.Timestamp = header.Timestamp
			resp = append(resp, a2)

		}


	}

	return resp,nil

}


func (a *StateAPI) StateMinerVestingFundsByHeight(ctx context.Context, maddr address.Address, start abi.ChainEpoch,end abi.ChainEpoch,tsk types.TipSetKey) ([]map[string]interface{}, error) {
	ts, err := a.Chain.GetTipSetFromKey(tsk)
	if err != nil {
		return nil, xerrors.Errorf("loading tipset %s: %w", err)
	}



	act, err := a.StateManager.LoadActor(ctx, maddr, ts)
	if err != nil {
		return nil, xerrors.Errorf("failed to load miner actor: %w", err)
	}

	mas, err := miner.Load(a.StateManager.ChainStore().ActorStore(ctx), act)
	if err != nil {
		return nil, xerrors.Errorf("failed to load miner actor state: %w", err)
	}

	vested, err := mas.LoadVestingFundList()
	if err != nil {
		return nil, err
	}

	//var res []map[string]interface{}
	//
	//for _, m := range vested {
	//	//if Epoch>=start && Epoch<=end{
	//	m2 := make(map[string]interface{})
	//	m2["Epoch"] =  m["Epoch"].(abi.ChainEpoch)
	//	m2["Amount"] = m["Amount"]
	//	res = append(res, m2)
	//	//}
	//}


	//abal, err := mas.AvailableBalance(act.Balance)
	//if err != nil {
	//	return types.EmptyInt, err
	//}

	return vested, nil
}


func (a *StateAPI) StateMessageByHeight(ctx context.Context, epoch abi.ChainEpoch) ([]*api.InvocResultExt, error) {
	var exts []*api.InvocResultExt

	tsk := types.EmptyTSK
	ts, err := a.Chain.GetTipSetFromKey(tsk)
	if err != nil {
		return nil, xerrors.Errorf("loading tipset %s: %w", err)
	}

	startTs, err := a.Chain.GetTipsetByHeight(ctx, abi.ChainEpoch(epoch), ts, true)
	if err != nil {
		return nil, xerrors.Errorf("loading tipset %s: %w", err)
	}

	_, t, err := stmgr.ComputeState(ctx, a.StateManager, startTs.Height(), nil, startTs)
	if err != nil {
		return nil, err
	}

	for _, result := range t {
		m := result.Msg
		invocResultExt := new(api.InvocResultExt)
		nMessage := new(api.NMessage)


		var flag = false
		var actor  *types.Actor
		actor, err = a.StateManager.LoadActorTsk(ctx, m.To, tsk)
		if err != nil {
			actor, err = a.StateGetActor(ctx,  m.To, tsk)
			if(err != nil){
				flag = true
			}
		}

		if m.Params != nil {
			if !flag {
				par, err := jsonParams(actor.Code, m.Method, m.Params)

				if err != nil {
					return nil, xerrors.Errorf("failed to get jsonParams: (%s):%d: %w", err)
				}
				nMessage.Params = par
			}
		}


		if result != nil {
			if result.MsgRct.Return !=nil {
				if !flag {
					returns, err := jsonReturn(actor.Code, m.Method, result.MsgRct.Return)
					if err != nil {
						return nil, xerrors.Errorf("failed to get jsonParams: (%s):%d: %w", err)
					}
					result.MsgRct.Returns = returns

				}

			}

			nMessage.MsgRct = result.MsgRct

		}

		nMessage.Version = m.Version
		nMessage.To = m.To
		nMessage.From = m.From
		nMessage.Nonce = m.Nonce
		nMessage.Value  = m.Value
		nMessage.GasLimit = m.GasLimit
		nMessage.GasFeeCap = m.GasFeeCap
		nMessage.GasPremium = m.GasPremium
		nMessage.Method = m.Method
		nMessage.CID = result.MsgCid
		//nMessage.BlockId = result.BlockId
		//nMessage.Height = result.Height
		//nMessage.GasCost = result.GasCost
		//nMessage.ExecutionTrace = result.ExecutionTrace
		//nMessage.Duration = result.Duration
		//nMessage.Error = result.Error
		if !flag {
			nMessage.CodeMethod = builtin.ActorNameByCode(actor.Code)

		}
		invocResultExt.Timestamp = result.Timestamp
		invocResultExt.Msg = nMessage
		invocResultExt.MsgRct = result.MsgRct
		invocResultExt.Error = result.Error
		invocResultExt.Duration = result.Duration
		invocResultExt.GasCost = result.GasCost
		invocResultExt.MsgCid = result.MsgCid
		invocResultExt.ExecutionTrace = result.ExecutionTrace
		invocResultExt.BlockId = result.BlockId
		invocResultExt.Height = result.Height

		exts = append(exts, invocResultExt)

	}



	return exts,nil



}
func (a *StateAPI) StateGetMessageTest(ctx context.Context,height int) (int, error) {


	return height, nil
}


func (a *StateAPI) StateReplayNoTs(ctx context.Context, mc cid.Cid) (*api.InvocResult, error) {
	replay, err2 := a.StateReplay(ctx, types.EmptyTSK, mc)
	if err2 != nil {
		return nil, xerrors.Errorf("loading StateReplayNoTs %s: %w", err2)
	}
	return replay,nil
}


func (a *StateAPI) StateMarketStorageDealNoTs(ctx context.Context, dealId abi.DealID) (*api.MarketDeal, error) {
	onChain, err := a.StateMarketStorageDeal(ctx, dealId, types.EmptyTSK)
	if err != nil {
		return nil, xerrors.Errorf("loading StateMarketStorageDealNoTs %s: %w", err)
	}
	return onChain,nil
}

func (a *StateAPI) StateTest(ctx context.Context,parame api.StateTestParams) (abi.SectorQuality, error) {

	size := abi.SectorSize(parame.Size)
	duration := abi.ChainEpoch(parame.Duration)
	dealWeight := big.NewInt(parame.DealWeight)
	verifiedWeight := big.NewInt(parame.VerifiedWeight)

	log.Infof("size %d",size)
	log.Infof("duration %d",duration)
	log.Infof("dealWeight %v",dealWeight)
	log.Infof("verifiedWeight %v",verifiedWeight)


	sectorWeight := builtin.QAPowerForWeight(size, duration, dealWeight, verifiedWeight)

	return  sectorWeight,nil

}

func (m *StateAPI) StateSectorGetInfoQa(ctx context.Context, maddr address.Address, n abi.SectorNumber, tsk types.TipSetKey) (*api.StateSectorGetInfoQaResp, error) {
	ts, err := m.Chain.GetTipSetFromKey(tsk)
	if err != nil {
		return nil, xerrors.Errorf("loading tipset %s: %w", tsk, err)
	}
	info, err := stmgr.MinerSectorInfo(ctx, m.StateManager, maddr, n, ts)
	if err != nil {
		return nil, xerrors.Errorf("loading MinerSectorInfo %s: %w", tsk, err)
	}
	minerInfo, err := m.StateMinerInfo(ctx, maddr, tsk)
	if err != nil {
		return nil, xerrors.Errorf("loading minerInfo %s: %w", tsk, err)
	}

	duration :=  info.Expiration-info.Activation


	sectorWeight := builtin.QAPowerForWeight(minerInfo.SectorSize, duration, info.DealWeight, info.VerifiedDealWeight)

	a := new(api.StateSectorGetInfoQaResp)
	a.SectorOnChainInfo = info
	a.SectorQuality = sectorWeight

	return a,nil
}


func (a *StateAPI) StateMinerSectorsQa(ctx context.Context, addr address.Address, sectorNos *bitfield.BitField, tsk types.TipSetKey) ([]*api.SectorOnChainInfoQa, error) {
	act, err := a.StateManager.LoadActorTsk(ctx, addr, tsk)
	if err != nil {
		return nil, xerrors.Errorf("failed to load miner actor: %w", err)
	}

	mas, err := miner.Load(a.StateManager.ChainStore().ActorStore(ctx), act)
	if err != nil {
		return nil, xerrors.Errorf("failed to load miner actor state: %w", err)
	}

	sectors, err := mas.LoadSectors(sectorNos)

	if err  != nil {
		return nil, xerrors.Errorf("failed to  LoadSectors : %w", err)
	}

	minerInfo, err := a.StateMinerInfo(ctx, addr, tsk)
	if err != nil {
		return nil, xerrors.Errorf("loading minerInfo %s: %w", tsk, err)
	}
	
	var res []*api.SectorOnChainInfoQa

	for _, sector := range sectors {
		a2 := new(api.SectorOnChainInfoQa)
		duration :=  sector.Expiration-sector.Activation

		sectorWeight := builtin.QAPowerForWeight(minerInfo.SectorSize, duration, sector.DealWeight, sector.VerifiedDealWeight)


		a2.SectorNumber = sector.SectorNumber
		a2.SealProof = sector.SealProof
		a2.SealedCID = sector.SealedCID
		a2.DealIDs = sector.DealIDs
		a2.Activation = sector.Activation
		a2.Expiration = sector.Expiration
		a2.DealWeight = sector.DealWeight
		a2.VerifiedDealWeight = sector.VerifiedDealWeight
		a2.InitialPledge = sector.InitialPledge
		a2.ExpectedDayReward = sector.ExpectedDayReward
		a2.ExpectedStoragePledge = sector.ExpectedStoragePledge
		a2.SectorQuality = sectorWeight

		res = append(res,a2 )
	}

	return res,nil
}




func (a *StateAPI) StateWithdrawal(ctx context.Context, params *api.StateWithDrawalReq) (string, error) {
	fmt.Print("StateWithdrawal:",params)
	//str :=  params.Root+" "+params.Miner+" "+params.EpochPrice+" "+params.MinBlocksDuration
	//fmt.Print("ClientStartDealGoReq:"+str)
	cmd := exec.Command("./lotus", "send", "--from",params.From, params.To ,params.Amount)

	var out bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &stderr
	err := cmd.Run()
	if err != nil {
		return "", xerrors.Errorf("failed lotus send: %w", err.Error(), stderr.String())
	} else {
		log.Info(out.String())
	}
	return  out.String(),nil
}


func (a *StateAPI) StateGetMessage(ctx context.Context, params cid.Cid) (*api.NMessage, error) {

	cm, err := a.Chain.GetCMessage(params)
	if err != nil {
		return nil, err
	}
	sm := cm.VMMessage()

	nMessage  := new(api.NMessage)
	nMessage.Version = sm.Version
	nMessage.To = sm.To
	nMessage.From = sm.From
	nMessage.Nonce = sm .Nonce
	nMessage.Value = sm.Value
	nMessage.GasLimit = sm.GasLimit
	nMessage.GasFeeCap = sm.GasFeeCap
	nMessage.GasPremium = sm.GasPremium
	nMessage.Method = sm.Method
	nMessage.CID = sm.Cid()

	var flag = false
	var actor  *types.Actor
	tsk := types.EmptyTSK

	actor, err = a.StateManager.LoadActorTsk(ctx, nMessage.To, tsk)

	if err != nil {
		actor, err = a.StateGetActor(ctx, nMessage.To, tsk)
		if(err != nil){
			flag = true
		}
	}

	if !flag {
		nMessage.CodeMethod = builtin.ActorNameByCode(actor.Code)

	}

	//ts, err := a.Chain.GetTipSetFromKey(tsk)
	//if err != nil {
	//	return nil, xerrors.Errorf("loading tipset %s: %w", tsk, err)
	//}

	//receipt, err := a.StateManager.GetReceipt(ctx, params, ts)
	//
	//if err != nil {
	//	return nil, xerrors.Errorf("loading receipt %s: %w", tsk, err)
	//}
	//nMessage.GasUsed = receipt.GasUsed
	//nMessage.ExitCode = receipt.ExitCode

	return nMessage ,err

}


func (a *StateAPI) StateMpoolReplace(ctx context.Context, params cid.Cid) (string, error) {
	set := a.Chain.GetHeaviestTipSet()
	cids := set.Cids()

	block, err := a.Chain.GetBlock(cids[0])

	if err != nil {
		return "", xerrors.Errorf("failed lotus send: %w", err.Error(), err)
	}

	var num1 float64 = 1.2

	var num2 float64 = 1.5

	fee := block.ParentBaseFee

	endGasFeeCap := decimal.NewFromFloat(num1).Mul(decimal.NewFromFloat(float64(fee.Int64())))

	endGasFeeStr := strings.Split(endGasFeeCap.String(), ".")
	endGasFee :=  endGasFeeStr[0]


	message, err := a.Chain.GetCMessage(params)

	if err != nil {
		return "", xerrors.Errorf("failed lotus send: %w", err.Error(), err)
	}

	newInt := big.NewInt(message.VMMessage().GasPremium.Int64())

	mul := decimal.NewFromFloat(num2).Mul(decimal.NewFromFloat(float64(newInt.Int64())))


	gaspremiumStr := strings.Split(mul.String(), ".")
	gaspremium :=  gaspremiumStr[0]

	fmt.Print("gas-premiumss:",gaspremium)
	fmt.Print("gas-feecapss:",endGasFee)
	fmt.Print("StateMpoolReplace:",params)
	//str :=  params.Root+" "+params.Miner+" "+params.EpochPrice+" "+params.MinBlocksDuration
	//fmt.Print("ClientStartDealGoReq:"+str)
	cmd := exec.Command("./lotus", "mpool", "replace","--auto=false","--gas-premium",gaspremium,"--gas-feecap",endGasFee,params.String())

	var out bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &stderr
	err = cmd.Run()
	if err != nil {
		return "", xerrors.Errorf("failed lotus send: %w", err.Error(), stderr.String())
	} else {
		log.Info(out.String())
	}
	return  out.String(),nil

}


func (a *StateAPI) StateDecodeReturn(ctx context.Context, toAddr address.Address, method abi.MethodNum, params []byte) (string, error) {
	tsk := types.EmptyTSK
	var act  *types.Actor
	var err error
	act, err = a.StateManager.LoadActorTsk(ctx, toAddr, tsk)
	if err != nil {
		act, err = a.StateGetActor(ctx, toAddr, tsk)
		if err != nil {
			return "", err
		}
	}
	//if builtin.IsStorageMarketMinerActor(act.Code) {
	//	if method == market.Methods.PublishStorageDeals {
	//		var ret market.PublishStorageDealsReturn
	//		err := ret.UnmarshalCBOR(bytes.NewReader(params))
	//		if err != nil {
	//			return "", err
	//		}
	//		b, err := json.MarshalIndent(&ret, "", "  ")
	//		result:=string(b)
	//		return result, err
	//	}
	//}
	jsonReturn, err := jsonReturn(act.Code, method, params)
	if err != nil {
		return "", err
	}

	return jsonReturn,nil

}
