package stack

import (
	"github.com/easyremote/server/internal/auth"
	"github.com/easyremote/server/internal/store"
)

type authService = auth.Service

func newAuthService(st store.Store, secret []byte) (*auth.Service, error) {
	return auth.New(st, devSMS(), secret)
}
