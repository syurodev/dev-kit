package crypto

import (
	"bytes"
	"testing"
)

func TestDeriveKeyIsDeterministic(t *testing.T) {
	params, err := DefaultKDFParams()
	if err != nil {
		t.Fatalf("DefaultKDFParams: %v", err)
	}

	a, err := DeriveKey("correct horse", params)
	if err != nil {
		t.Fatalf("DeriveKey: %v", err)
	}
	b, err := DeriveKey("correct horse", params)
	if err != nil {
		t.Fatalf("DeriveKey: %v", err)
	}
	if !bytes.Equal(a, b) {
		t.Fatal("same password+params must derive the same key")
	}
	if len(a) != int(params.KeyLength) {
		t.Fatalf("key length = %d, want %d", len(a), params.KeyLength)
	}
}

func TestDeriveKeyDiffersBySalt(t *testing.T) {
	p1, _ := DefaultKDFParams()
	p2, _ := DefaultKDFParams()

	k1, _ := DeriveKey("pw", p1)
	k2, _ := DeriveKey("pw", p2)
	if bytes.Equal(k1, k2) {
		t.Fatal("different salts must derive different keys")
	}
}

func TestDeriveKeyRequiresPassword(t *testing.T) {
	params, _ := DefaultKDFParams()
	if _, err := DeriveKey("", params); err == nil {
		t.Fatal("empty password must error")
	}
}

func TestDeriveKeyRejectsUnsafeParams(t *testing.T) {
	base, err := DefaultKDFParams()
	if err != nil {
		t.Fatalf("DefaultKDFParams: %v", err)
	}

	cases := []struct {
		name   string
		mutate func(*KDFParams)
	}{
		{name: "memory too low", mutate: func(p *KDFParams) { p.Memory = 512 }},
		{name: "memory too high", mutate: func(p *KDFParams) { p.Memory = 2097153 }},
		{name: "iterations zero", mutate: func(p *KDFParams) { p.Iterations = 0 }},
		{name: "iterations too high", mutate: func(p *KDFParams) { p.Iterations = 11 }},
		{name: "parallelism zero", mutate: func(p *KDFParams) { p.Parallelism = 0 }},
		{name: "parallelism too high", mutate: func(p *KDFParams) { p.Parallelism = 17 }},
		{name: "key length too low", mutate: func(p *KDFParams) { p.KeyLength = 8 }},
		{name: "key length too high", mutate: func(p *KDFParams) { p.KeyLength = 65 }},
		{name: "salt too short", mutate: func(p *KDFParams) { p.Salt = []byte("short") }},
		{name: "salt too long", mutate: func(p *KDFParams) { p.Salt = bytes.Repeat([]byte{1}, 65) }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			params := base
			params.Salt = append([]byte(nil), base.Salt...)
			tc.mutate(&params)
			if _, err := DeriveKey("password", params); err == nil {
				t.Fatal("expected error")
			}
		})
	}
}

func TestDeriveKeyAcceptsDefaultParams(t *testing.T) {
	params, err := DefaultKDFParams()
	if err != nil {
		t.Fatalf("DefaultKDFParams: %v", err)
	}
	if _, err := DeriveKey("password", params); err != nil {
		t.Fatalf("DeriveKey default: %v", err)
	}
}
