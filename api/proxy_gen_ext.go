package api

import (
	"context"
	"github.com/filecoin-project/go-address"
	"github.com/filecoin-project/go-bitfield"
	"github.com/filecoin-project/go-state-types/abi"
	"github.com/filecoin-project/lotus/chain/types"
	das "github.com/filecoin-project/specs-actors/v5/actors/builtin/miner"
	"github.com/ipfs/go-cid"
)

func (c *FullNodeStruct) StateGetActorsInfo(ctx context.Context, key types.TipSetKey) (*ActorStates, error) {
	return c.Internal.StateGetActorsInfo(ctx, key)
}

func (c *FullNodeStruct) StateGetRewardLastPerEpochReward(ctx context.Context, key types.TipSetKey) (abi.TokenAmount, error) {
	return c.Internal.StateGetRewardLastPerEpochReward(ctx, key)
}

func (c *FullNodeStruct) StatePreCommittedSectors(ctx context.Context, addr address.Address, tsk types.TipSetKey) ([]uint64, error) {
	return c.Internal.StatePreCommittedSectors(ctx, addr, tsk)
}

func (c *FullNodeStruct) StateMinerSectorNumbers(ctx context.Context, addr address.Address, tsk types.TipSetKey) (num []uint64, err error) {
	return c.Internal.StateMinerSectorNumbers(ctx, addr, tsk)
}

func (c *FullNodeStruct) StateBlockMessages(ctx context.Context, b cid.Cid) (*StateBlockMessagesRes, error) {
	return c.Internal.StateBlockMessages(ctx, b)
}

func (c *FullNodeStruct) StateGetParentDeals(ctx context.Context, b cid.Cid) ([]*StateGetParentDealsResp, error) {
	return c.Internal.StateGetParentDeals(ctx, b)
}

func (c *FullNodeStruct) StateReplayList(ctx context.Context, tsp *types.TipSet) ([]*InvocResult, error) {
	return c.Internal.StateReplayList(ctx, tsp)
}

func (c *FullNodeStruct) StateReplayLists(ctx context.Context, height int64) ([]*InvocResult, error) {
	return c.Internal.StateReplayLists(ctx, height)
}

func (c *FullNodeStruct) StateMinerPartitionsNew(ctx context.Context, m address.Address, dlIdx uint64, tsk types.TipSetKey) ([]*das.Partition, error) {
	return c.Internal.StateMinerPartitionsNew(ctx, m, dlIdx, tsk)
}

func (c *FullNodeStruct) StateGetParentReceipts(ctx context.Context, b cid.Cid) ([]*types.MessageReceipt, error) {
	return c.Internal.StateGetParentReceipts(ctx, b)
}

func (c *FullNodeStruct) StateMinerDeadlinesByMiner(ctx context.Context, m address.Address, tsk types.TipSetKey) ([]*StateMinerDeadlinesByMinerInfo, error) {
	return c.Internal.StateMinerDeadlinesByMiner(ctx, m, tsk)
}

func (c *FullNodeStruct) WalletBalanceByTs(ctx context.Context, a address.Address, tsk types.TipSetKey) (types.BigInt, error) {
	return c.Internal.WalletBalanceByTs(ctx, a, tsk)
}


func (c *FullNodeStruct) StateCallExt(ctx context.Context, msg *types.Message, priorMsgs types.ChainMsg,tsk types.TipSetKey) (*InvocResult, error) {
	return c.Internal.StateCallExt(ctx, msg, priorMsgs,tsk)
}

func (c *FullNodeStruct) StateReplayTest(ctx context.Context, tsk types.TipSetKey, cids []cid.Cid) ([]*InvocResult, error) {
	return c.Internal.StateReplayTest(ctx, tsk,cids)
}

func (c *FullNodeStruct) StateReplayEx(ctx context.Context, tsk types.TipSetKey) ([]*InvocResults, error) {
	return c.Internal.StateReplayEx(ctx, tsk)
}




func (c *FullNodeStruct) StateComputeBaseFee(ctx context.Context, b cid.Cid) (abi.TokenAmount, error) {
	return c.Internal.StateComputeBaseFee(ctx, b)
}


func (c *FullNodeStruct) StateGetMessageByHeightInterval(ctx context.Context, start int64) ([]*NMessage, error) {
	return c.Internal.StateGetMessageByHeightInterval(ctx, start)
}

func (c *FullNodeStruct) StateGetBlockByHeightInterval(ctx context.Context, start int64) ([]*types.BlockHeaders, error) {
	return c.Internal.StateGetBlockByHeightInterval(ctx, start)
}

func (c *FullNodeStruct) StateGetBlockByHeightWhere(ctx context.Context, params StateGetBlockByHeightIntervalParams) ([]*types.BlockHeaders, error) {
	return c.Internal.StateGetBlockByHeightWhere(ctx, params)
}

func (c *FullNodeStruct) StateGetMessageDealsByHeightInterval(ctx context.Context, start int64) ([]*StateGetParentDealsResp, error) {
	return c.Internal.StateGetMessageDealsByHeightInterval(ctx, start)
}

func (c *FullNodeStruct) StateTest(ctx context.Context,parame StateTestParams) (abi.SectorQuality, error) {
	return c.Internal.StateTest(ctx,parame)
}

func (c *FullNodeStruct) StateSectorGetInfoQa(ctx context.Context, address address.Address, sectorNumber abi.SectorNumber, tipSetKey types.TipSetKey) (*StateSectorGetInfoQaResp, error) {
	return c.Internal.StateSectorGetInfoQa(ctx,address,sectorNumber,tipSetKey)
}

func (c *FullNodeStruct) StateGetBlockByHeight(ctx context.Context, start int64, end int64) ([]*Blocks, error) {
	return c.Internal.StateGetBlockByHeight(ctx, start, end)
}


func (c *FullNodeStruct) StateMinerVestingFundsByHeight(ctx context.Context, maddr address.Address, start abi.ChainEpoch,end abi.ChainEpoch,tsk types.TipSetKey) ([]map[string]interface{}, error) {
	return c.Internal.StateMinerVestingFundsByHeight(ctx, maddr, start,end,tsk)
}


func (c *FullNodeStruct) StateMessageByHeight(ctx context.Context,  epoch abi.ChainEpoch) ([]*InvocResultExt, error) {
	return c.Internal.StateMessageByHeight(ctx, epoch)
}

func (c *FullNodeStruct) ClientIpfsAddGo(ctx context.Context,clientIpfsAddParams  ClientIpfsAddGoParams) (ClientIpfsAddGoParams, error) {
	return c.Internal.ClientIpfsAddGo(ctx, clientIpfsAddParams)
}


func (c *FullNodeStruct) StateGetMessageByHeightIntervalEx(ctx context.Context, params StateGetMessageByHeightIntervalParams) ([]*SMessage, error) {
	return c.Internal.StateGetMessageByHeightIntervalEx(ctx, params)
}

func (c *FullNodeStruct) StateComputeBaseFeeTest(ctx context.Context) (abi.TokenAmount, error) {
	return c.Internal.StateComputeBaseFeeTest(ctx)
}

func (s *FullNodeStruct) StateReplayNoTs(p0 context.Context, p2 cid.Cid) (*InvocResult, error) {
	return s.Internal.StateReplayNoTs(p0, p2)
}


func (s *FullNodeStruct) StateMarketStorageDealNoTs(p0 context.Context, p1 abi.DealID) (*MarketDeal, error) {
	return s.Internal.StateMarketStorageDealNoTs(p0, p1)
}

func (c *FullNodeStruct) StateWithdrawal(ctx context.Context, params *StateWithDrawalReq) (string, error) {
	return c.Internal.StateWithdrawal(ctx, params)
}

func (c *FullNodeStruct) StateGetMessage(ctx context.Context, params cid.Cid) (*NMessage, error) {
	return c.Internal.StateGetMessage(ctx, params)
}

func (c *FullNodeStruct) StateMpoolReplace(ctx context.Context, params cid.Cid) (string, error) {
	return c.Internal.StateMpoolReplace(ctx, params)
}

func (s *FullNodeStruct) StateDecodeReturn(p0 context.Context, p1 address.Address, p2 abi.MethodNum, p3 []byte) (string, error) {
	return s.Internal.StateDecodeReturn(p0, p1, p2, p3)
}

func (s *FullNodeStruct) StateMinerSectorsQa(p0 context.Context, p1 address.Address, p2 *bitfield.BitField, p3 types.TipSetKey) ([]*SectorOnChainInfoQa, error) {
	if s.Internal.StateMinerSectors == nil {
		return *new([]*SectorOnChainInfoQa), ErrNotSupported
	}
	return s.Internal.StateMinerSectorsQa(p0, p1, p2, p3)
}