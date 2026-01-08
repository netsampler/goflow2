package kafka

// From https://github.com/Shopify/sarama/blob/main/examples/sasl_scram_client/scram_client.go

import (
	"crypto/sha256"
	"crypto/sha512"

	"github.com/xdg-go/scram"
)

var (
	// SHA256 is the SCRAM hash generator for SHA-256.
	SHA256 scram.HashGeneratorFcn = sha256.New
	// SHA512 is the SCRAM hash generator for SHA-512.
	SHA512 scram.HashGeneratorFcn = sha512.New
)

// XDGSCRAMClient implements sarama's SCRAM client interface.
type XDGSCRAMClient struct {
	*scram.Client
	*scram.ClientConversation
	scram.HashGeneratorFcn
}

// Begin initializes a SCRAM conversation.
func (x *XDGSCRAMClient) Begin(userName, password, authzID string) (err error) {
	x.Client, err = x.NewClient(userName, password, authzID)
	if err != nil {
		return err
	}
	x.ClientConversation = x.NewConversation()
	return nil
}

// Step advances the SCRAM conversation.
func (x *XDGSCRAMClient) Step(challenge string) (response string, err error) {
	response, err = x.ClientConversation.Step(challenge)
	return
}

// Done reports whether the SCRAM conversation is complete.
func (x *XDGSCRAMClient) Done() bool {
	return x.ClientConversation.Done()
}
