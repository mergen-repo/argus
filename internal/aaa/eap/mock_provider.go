package eap

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
)

type MockVectorProvider struct{}

func NewMockVectorProvider() *MockVectorProvider {
	return &MockVectorProvider{}
}

func (p *MockVectorProvider) GetSIMTriplets(_ context.Context, imsi string) (*SIMTriplets, error) {
	triplets := &SIMTriplets{}
	seed := sha256.Sum256([]byte("sim-seed:" + imsi))

	for i := 0; i < 3; i++ {
		copy(triplets.RAND[i][:], deterministicRand(seed[:], i, 16))
		copy(triplets.SRES[i][:], deterministicRand(seed[:], i+10, 4))
		copy(triplets.Kc[i][:], deterministicRand(seed[:], i+20, 8))
	}

	return triplets, nil
}

func (p *MockVectorProvider) GetAKAQuintets(_ context.Context, imsi string) (*AKAQuintets, error) {
	quintets := &AKAQuintets{}
	seed := sha256.Sum256([]byte("aka-seed:" + imsi))

	copy(quintets.RAND[:], deterministicRand(seed[:], 0, 16))
	copy(quintets.AUTN[:], deterministicRand(seed[:], 1, 16))
	quintets.XRES = deterministicRand(seed[:], 2, 8)
	copy(quintets.CK[:], deterministicRand(seed[:], 3, 16))
	copy(quintets.IK[:], deterministicRand(seed[:], 4, 16))

	return quintets, nil
}

func deterministicRand(seed []byte, index int, length int) []byte {
	input := make([]byte, len(seed)+1)
	copy(input, seed)
	input[len(seed)] = byte(index)
	h := sha256.Sum256(input)
	if length > 32 {
		length = 32
	}
	return h[:length]
}

type RandomVectorProvider struct{}

func NewRandomVectorProvider() *RandomVectorProvider {
	return &RandomVectorProvider{}
}

func (p *RandomVectorProvider) GetSIMTriplets(_ context.Context, _ string) (*SIMTriplets, error) {
	triplets := &SIMTriplets{}
	for i := 0; i < 3; i++ {
		rand.Read(triplets.RAND[i][:])
		rand.Read(triplets.SRES[i][:])
		rand.Read(triplets.Kc[i][:])
	}
	return triplets, nil
}

func (p *RandomVectorProvider) GetAKAQuintets(_ context.Context, _ string) (*AKAQuintets, error) {
	quintets := &AKAQuintets{}
	rand.Read(quintets.RAND[:])
	rand.Read(quintets.AUTN[:])
	quintets.XRES = make([]byte, 8)
	rand.Read(quintets.XRES)
	rand.Read(quintets.CK[:])
	rand.Read(quintets.IK[:])
	return quintets, nil
}
