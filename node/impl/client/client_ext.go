package client

import (
	"context"
	"fmt"
	"github.com/filecoin-project/lotus/api"
	"golang.org/x/xerrors"
	"os/exec"
)

func (a *API)  ClientIpfsAddGo(ctx context.Context,clientIpfsAddParams  api.ClientIpfsAddGoParams ) (api.ClientIpfsAddGoParams, error){
		cmd := exec.Command("ipfs", "add", clientIpfsAddParams.Path)
		output,err := cmd.Output()
		if err != nil {
			return clientIpfsAddParams, xerrors.Errorf("failed ipfs add ID: %w", err)
		}
		s := string(output)
	fmt.Print("QMHash:",s)
	clientIpfsAddParams.Hash = s
	return clientIpfsAddParams,err
}