package tsunami

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func BenchmarkSyntax(b *testing.B) {
	for i := 0; i < b.N; i++ {
		tsunamiSyntax(`
output text/csv
---
payload
		`)
	}
}

func TestSyntaxTransfrom(t *testing.T) {
	t.Run("basic", func(t *testing.T) {
		header, payload, output := tsunamiSyntax(`payload`)
		t.Logf("header: %+v %+v", header, payload)
		assert.Equal(t, string(header), "")
		assert.Equal(t, string(payload), "payload")
		assert.Equal(t, string(output), "application/json")
	})

	t.Run("basic oneline", func(t *testing.T) {
		header, payload, output := tsunamiSyntax(`output text/csv --- payload`)
		// t.Logf("header: %+v %+v", payload, []byte("payload"))
		assert.Equal(t, string(header), "")
		assert.Equal(t, string(payload), "payload")
		assert.Equal(t, string(output), "text/csv")
	})
	t.Run("basic multiline", func(t *testing.T) {
		header, payload, output := tsunamiSyntax(`output text/csv
		---
		payload
		
		`)
		// t.Logf("header: %+v %+v", payload, []byte("payload"))
		assert.Equal(t, string(header), "")
		assert.Equal(t, string(payload), "payload")
		assert.Equal(t, string(output), "text/csv")
	})
}
