package eap

import (
	"context"
	"errors"
	"fmt"

	"github.com/btopcu/argus/internal/operator/adapter"
	"github.com/rs/zerolog"
)

var (
	ErrInsufficientTriplets = errors.New("eap: insufficient triplet vectors from adapter")
	ErrInsufficientQuintets = errors.New("eap: insufficient quintet vectors from adapter")
	ErrNoTripletVectors     = errors.New("eap: no triplet vectors returned by adapter")
	ErrNoQuintetVectors     = errors.New("eap: no quintet vectors returned by adapter")
)

type AdapterVectorProvider struct {
	adapter adapter.Adapter
	logger  zerolog.Logger
}

func NewAdapterVectorProvider(a adapter.Adapter, logger zerolog.Logger) *AdapterVectorProvider {
	return &AdapterVectorProvider{
		adapter: a,
		logger:  logger.With().Str("component", "eap_adapter_provider").Logger(),
	}
}

func (p *AdapterVectorProvider) GetSIMTriplets(ctx context.Context, imsi string) (*SIMTriplets, error) {
	vectors, err := p.adapter.FetchAuthVectors(ctx, imsi, 6)
	if err != nil {
		return nil, fmt.Errorf("fetch SIM triplets from adapter: %w", err)
	}

	var tripletVectors []adapter.AuthVector
	for _, v := range vectors {
		if v.Type == adapter.VectorTypeTriplet {
			tripletVectors = append(tripletVectors, v)
		}
	}

	if len(tripletVectors) == 0 {
		return nil, ErrNoTripletVectors
	}

	if len(tripletVectors) < 3 {
		return nil, fmt.Errorf("%w: got %d, need 3", ErrInsufficientTriplets, len(tripletVectors))
	}

	triplets := &SIMTriplets{}
	for i := 0; i < 3; i++ {
		copy(triplets.RAND[i][:], tripletVectors[i].RAND)
		copy(triplets.SRES[i][:], tripletVectors[i].SRES)
		copy(triplets.Kc[i][:], tripletVectors[i].Kc)
	}

	p.logger.Debug().
		Str("imsi", imsi).
		Int("vectors_fetched", len(vectors)).
		Int("triplets_used", 3).
		Msg("SIM triplets fetched from adapter")

	return triplets, nil
}

func (p *AdapterVectorProvider) GetAKAQuintets(ctx context.Context, imsi string) (*AKAQuintets, error) {
	vectors, err := p.adapter.FetchAuthVectors(ctx, imsi, 2)
	if err != nil {
		return nil, fmt.Errorf("fetch AKA quintets from adapter: %w", err)
	}

	var quintetVectors []adapter.AuthVector
	for _, v := range vectors {
		if v.Type == adapter.VectorTypeQuintet {
			quintetVectors = append(quintetVectors, v)
		}
	}

	if len(quintetVectors) == 0 {
		return nil, ErrNoQuintetVectors
	}

	q := quintetVectors[0]
	quintets := &AKAQuintets{}
	copy(quintets.RAND[:], q.RAND)
	copy(quintets.AUTN[:], q.AUTN)
	quintets.XRES = make([]byte, len(q.XRES))
	copy(quintets.XRES, q.XRES)
	copy(quintets.CK[:], q.CK)
	copy(quintets.IK[:], q.IK)

	p.logger.Debug().
		Str("imsi", imsi).
		Int("vectors_fetched", len(vectors)).
		Msg("AKA quintets fetched from adapter")

	return quintets, nil
}
