package utils

import "flag"

// updateFixtures is the package-level flag shared by every parity-fixture
// regeneration test in x-go/utils (phone, and — once rebased on
// this file — backoff and ip). Each parity test reads *updateFixtures to
// decide whether to rewrite its testdata/*.json fixture in lockstep with
// the corresponding Java conformance test on the templates/game side.
//
// Centralising the declaration here is load-bearing: the package contains
// multiple parity tests (one per ported utility) that all need the same
// `-update-fixtures` switch, and Go forbids declaring the same
// package-level identifier in two different *_test.go files. Per-file
// declarations compile in isolation but collide the moment a second
// parity test lands in the same package, which is why every parity PR
// in C4b Wave 1 must reference this single flag instead of declaring its
// own.
var updateFixtures = flag.Bool(
	"update-fixtures",
	false,
	"regenerate x-go/utils/testdata/*.json fixtures from in-test cases (parity with Java conformance JSON)",
)
