package utils

import "context"

type FlowPipe interface {
	DecodeFlow(ctx context.Context, msg interface{}) error
	Close()
}
