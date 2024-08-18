package debug

import (
	"runtime/debug"

	"github.com/netsampler/goflow2/v2/utils"
)

func PanicDecoderWrapper(wrapped utils.DecoderFunc) utils.DecoderFunc {
	return func(msg interface{}) (err error) {
		defer func() {
			if pErr := recover(); pErr != nil {
				pErrC, _ := pErr.(string)
				err = &PanicErrorMessage{Msg: msg, Inner: pErrC, Stacktrace: debug.Stack()}
			}
		}()
		err = wrapped(msg)
		return err
	}
}
