package radius

import (
	"testing"

	"github.com/rs/zerolog"
	radius "layeh.com/radius"
)

var (
	benchIMEISink string
	benchSVSink   string
	benchOKSink   bool
)

func BenchmarkExtract3GPPIMEISV_RADIUS(b *testing.B) {
	vsa := buildIMEISVVSA([]byte("359211089765432,01"))
	pkt := radius.New(radius.CodeAccessRequest, []byte("testing123"))
	pkt.Attributes.Add(radius.Type(26), radius.Attribute(vsa))
	nop := zerolog.Nop()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		benchIMEISink, benchSVSink, benchOKSink = Extract3GPPIMEISV(pkt, nop, nil)
	}
}
