package party

import (
	"context"
)

type Party struct {
}

func NewParty() *Party {
	return &Party{}
}

func (party *Party) GetVersion(ctx context.Context, in *Empty) (*Version, error) {
	return &Version{"1.0.0"}, nil
}

