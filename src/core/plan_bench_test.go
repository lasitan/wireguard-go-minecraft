package core

import (
	"fmt"
	"testing"
)

func bigMesh(n int) Mesh {
	m := Mesh{Revision: 1}
	m.Nodes = append(m.Nodes, mother("m0", "M0", "10.10.0.1/16", "1.1.1.1"))
	m.Nodes = append(m.Nodes, mother("m1", "M1", "10.20.0.1/16", "2.2.2.2"))
	for i := 0; i < n; i++ {
		sub := "10.10"
		if i%2 == 1 {
			sub = "10.20"
		}
		m.Nodes = append(m.Nodes, child(fmt.Sprintf("c%d", i), fmt.Sprintf("C%d", i),
			fmt.Sprintf("%s.%d.%d/16", sub, 1+i/250, 1+i%250), "10.10.0.0/16", "10.20.0.0/16"))
	}
	return m
}

func BenchmarkPlanMesh1000(b *testing.B) {
	m := bigMesh(1000)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		PlanMesh(&m)
	}
}

func BenchmarkDesiredOne1000(b *testing.B) {
	m := bigMesh(1000)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := CompileDesired(&m, "c7", DesiredDefaults{}); err != nil {
			b.Fatal(err)
		}
	}
}
