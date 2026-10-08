package webhook

import "flag"

// updateFixtures is the package-level flag shared by every parity-fixture
// regeneration test in x-go/webhook. Analogous to the same flag in
// x-go/utils/fixture_flags_test.go.
//
// Run: cd x-go/webhook && go test -run TestUpdateFixtures -update-fixtures .
//
// This writes x-go/webhook/testdata/signing_vectors.json from the
// in-test case table, which must then be copied to
// templates/game/backend/platform-core/platform-common/
// src/test/resources/conformance/webhook_signing_vectors.json
// so the Java conformance test stays in lockstep.
var updateFixtures = flag.Bool(
	"update-fixtures",
	false,
	"regenerate x-go/webhook/testdata/signing_vectors.json fixtures from in-test cases (parity with Java conformance JSON)",
)
