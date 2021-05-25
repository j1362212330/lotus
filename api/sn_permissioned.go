package api

import (
	"github.com/filecoin-project/go-jsonrpc/auth"
)

func PermissionedWorkerSnAPI(a WorkerSnAPI) WorkerSnAPI {
	var out WorkerSnAPIStruct
	auth.PermissionedProxy(AllPermissions, DefaultPerms, a, &out.Internal)
	return &out
}
