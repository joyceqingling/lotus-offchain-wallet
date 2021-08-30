package v0api

import (
	"context"
	"github.com/filecoin-project/go-address"
	"github.com/filecoin-project/go-state-types/abi"
	"github.com/filecoin-project/lotus/api"
	"github.com/filecoin-project/lotus/chain/types"
	das "github.com/filecoin-project/specs-actors/v5/actors/builtin/miner"
	"github.com/ipfs/go-cid"
)

func (c *FullNodeStruct) StateGetActorsInfo(ctx context.Context, key types.TipSetKey) (*api.ActorStates, error) {
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

func (c *FullNodeStruct) StateBlockMessages(ctx context.Context, b cid.Cid) (*api.StateBlockMessagesRes, error) {
	return c.Internal.StateBlockMessages(ctx, b)
}

func (c *FullNodeStruct) StateGetParentDeals(ctx context.Context, b cid.Cid) ([]*api.StateGetParentDealsResp, error) {
	return c.Internal.StateGetParentDeals(ctx, b)
}

func (c *FullNodeStruct) StateReplayList(ctx context.Context, tsp *types.TipSet) ([]*api.InvocResult, error) {
	return c.Internal.StateReplayList(ctx, tsp)
}

func (c *FullNodeStruct) StateReplayLists(ctx context.Context, height int64) ([]*api.InvocResult, error) {
	return c.Internal.StateReplayLists(ctx, height)
}

func (c *FullNodeStruct) StateMinerPartitionsNew(ctx context.Context, m address.Address, dlIdx uint64, tsk types.TipSetKey) ([]*das.Partition, error) {
	return c.Internal.StateMinerPartitionsNew(ctx, m, dlIdx, tsk)
}

func (c *FullNodeStruct) StateGetParentReceipts(ctx context.Context, b cid.Cid) ([]*types.MessageReceipt, error) {
	return c.Internal.StateGetParentReceipts(ctx, b)
}

func (c *FullNodeStruct) StateMinerDeadlinesByMiner(ctx context.Context, m address.Address, tsk types.TipSetKey) ([]*api.StateMinerDeadlinesByMinerInfo, error) {
	return c.Internal.StateMinerDeadlinesByMiner(ctx, m, tsk)
}

func (c *FullNodeStruct) WalletBalanceByTs(ctx context.Context, a address.Address, tsk types.TipSetKey) (types.BigInt, error) {
	return c.Internal.WalletBalanceByTs(ctx, a, tsk)
}


func (c *FullNodeStruct) StateCallExt(ctx context.Context, msg *types.Message, priorMsgs types.ChainMsg,tsk types.TipSetKey) (*api.InvocResult, error) {
	return c.Internal.StateCallExt(ctx, msg, priorMsgs,tsk)
}

func (c *FullNodeStruct) StateReplayTest(ctx context.Context, tsk types.TipSetKey, cids []cid.Cid) ([]*api.InvocResult, error) {
	return c.Internal.StateReplayTest(ctx, tsk,cids)
}

func (c *FullNodeStruct) StateReplayEx(ctx context.Context, tsk types.TipSetKey) ([]*api.InvocResults, error) {
	return c.Internal.StateReplayEx(ctx, tsk)
}




func (c *FullNodeStruct) StateComputeBaseFee(ctx context.Context, b cid.Cid) (abi.TokenAmount, error) {
	return c.Internal.StateComputeBaseFee(ctx, b)
}


func (c *FullNodeStruct) StateGetMessageByHeightInterval(ctx context.Context, start int64) ([]*api.NMessage, error) {
	return c.Internal.StateGetMessageByHeightInterval(ctx, start)
}

func (c *FullNodeStruct) StateGetBlockByHeightInterval(ctx context.Context, start int64) ([]*types.BlockHeaders, error) {
	return c.Internal.StateGetBlockByHeightInterval(ctx, start)
}

func (c *FullNodeStruct) StateGetBlockByHeightWhere(ctx context.Context, params api.StateGetBlockByHeightIntervalParams) ([]*types.BlockHeaders, error) {
	return c.Internal.StateGetBlockByHeightWhere(ctx, params)
}

func (c *FullNodeStruct) StateGetMessageDealsByHeightInterval(ctx context.Context, start int64) ([]*api.StateGetParentDealsResp, error) {
	return c.Internal.StateGetMessageDealsByHeightInterval(ctx, start)
}

func (c *FullNodeStruct) StateGetBlockByHeight(ctx context.Context, start int64, end int64) ([]*api.Blocks, error) {
	return c.Internal.StateGetBlockByHeight(ctx, start, end)
}


func (c *FullNodeStruct) StateMinerVestingFundsByHeight(ctx context.Context, maddr address.Address, start abi.ChainEpoch,end abi.ChainEpoch,tsk types.TipSetKey) ([]map[string]interface{}, error) {
	return c.Internal.StateMinerVestingFundsByHeight(ctx, maddr, start,end,tsk)
}


func (c *FullNodeStruct) StateMessageByHeight(ctx context.Context,  epoch abi.ChainEpoch) ([]*api.InvocResultExt, error) {
	return c.Internal.StateMessageByHeight(ctx, epoch)
}

func (c *FullNodeStruct) ClientIpfsAddGo(ctx context.Context,clientIpfsAddParams  api.ClientIpfsAddGoParams) (api.ClientIpfsAddGoParams, error) {
	return c.Internal.ClientIpfsAddGo(ctx, clientIpfsAddParams)
}


func (c *FullNodeStruct) StateGetMessageByHeightIntervalEx(ctx context.Context, params api.StateGetMessageByHeightIntervalParams) ([]*api.SMessage, error) {
	return c.Internal.StateGetMessageByHeightIntervalEx(ctx, params)
}

func (c *FullNodeStruct) StateComputeBaseFeeTest(ctx context.Context) (abi.TokenAmount, error) {
	return c.Internal.StateComputeBaseFeeTest(ctx)
}

func (s *FullNodeStruct) StateReplayNoTs(p0 context.Context, p2 cid.Cid) (*api.InvocResult, error) {
	return s.Internal.StateReplayNoTs(p0, p2)
}


func (s *FullNodeStruct) StateMarketStorageDealNoTs(p0 context.Context, p1 abi.DealID) (*api.MarketDeal, error) {
	return s.Internal.StateMarketStorageDealNoTs(p0, p1)
}

func (c *FullNodeStruct) StateSectorGetInfoQa(ctx context.Context, address address.Address, sectorNumber abi.SectorNumber, tipSetKey types.TipSetKey) (*api.StateSectorGetInfoQaResp, error) {
	return c.Internal.StateSectorGetInfoQa(ctx,address,sectorNumber,tipSetKey)
}


