package rbac

import "flag"

// updateFixtures is the package-level flag shared by every parity-fixture
// regeneration test in x-go/rbac. Each parity test reads *updateFixtures
// to decide whether to rewrite its testdata/*.json fixture in lockstep with
// the corresponding Java conformance test on the templates/game side.
//
// Centralising the declaration here is load-bearing: Go forbids declaring the
// same package-level identifier in two different *_test.go files within the
// same package. Per the canonical pattern established in x-go/utils by
// C4b Wave 1 PR #231, the flag lives in a dedicated file that every parity
// test file in this package reads via *updateFixtures.
var updateFixtures = flag.Bool(
	"update-fixtures",
	false,
	"regenerate x-go/rbac/testdata/*.json fixtures from in-test cases (parity with Java conformance JSON)",
)
