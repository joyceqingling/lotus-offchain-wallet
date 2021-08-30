package full

import (
	"github.com/filecoin-project/specs-actors/actors/builtin"
	"github.com/ipfs/go-cid"
)

func BuiltinName1(code cid.Cid)  string{
	return builtin.ActorNameByCode(code)
}
