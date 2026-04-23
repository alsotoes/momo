package common

import (
	"testing"

	"gopkg.in/ini.v1"
)

func BenchmarkLoadGlobalConfig(b *testing.B) {
	cfg, _ := ini.Load([]byte(`
[global]
debug = true
replication_order = 1,2,3,4,5,6,7,8,9,10,11,12,13,14,15,16,17,18,19,20
polymorphic_system = true
`))
	section := cfg.Section("global")

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := loadGlobalConfig(section)
		if err != nil {
			b.Fatal(err)
		}
	}
}
