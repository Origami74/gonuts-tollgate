package nut09

import "github.com/Origami74/gonuts-tollgate/cashu"

type PostRestoreRequest struct {
	Outputs cashu.BlindedMessages `json:"outputs"`
}

type PostRestoreResponse struct {
	Outputs    cashu.BlindedMessages   `json:"outputs"`
	Signatures cashu.BlindedSignatures `json:"signatures"`
}
