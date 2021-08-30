package full

import (
	"github.com/filecoin-project/specs-actors/v4/actors/builtin"
	"github.com/ipfs/go-cid"
)

func BuiltinName4(code cid.Cid)  string{
	return builtin.ActorNameByCode(code)
}
