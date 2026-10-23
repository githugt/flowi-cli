// SPDX-License-Identifier: AGPL-3.0-or-later

package cli

import (
	"strings"
	"testing"

	"github.com/githugt/flowi-lib/analysis"
)

func TestWriteImpactText_should_show_consumers_and_failing_reads(t *testing.T) {
	rep := analysis.Report{Collection: "identity", Request: "auth/login", Status: analysis.Broken, Versions: []analysis.VersionUsage{
		{Version: "1", Contract: analysis.ContractInfo{}, Consumers: []analysis.Consumer{}},
		{Version: "2", Contract: analysis.ContractInfo{Source: analysis.SourceExample, Files: []string{"/c/requests/auth/login.examples/v2-200.json"}},
			Consumers: []analysis.Consumer{
				{Kind: analysis.KindNode, Collection: "onboarding", Flow: "signup", Node: "login", Status: analysis.Broken, Reads: []analysis.Read{
					{Kind: analysis.ReadJSONPath, Expr: "nodes.login.token", Target: "$.token", Reader: "after", Status: analysis.Broken, Reason: "not in example v2-200.json"},
					{Kind: analysis.ReadInput, Expr: "with.tenant", Target: "tenant", Reader: "login", Status: analysis.Broken, Reason: "nothing here fills it"},
					{Kind: analysis.ReadHeader, Expr: "nodes.login.headers.X-Id", Target: "X-Id", Reader: "hdr", Status: analysis.Compatible},
				}},
				{Kind: analysis.KindHook, Collection: "identity", Hook: "post · collection identity · inline #0", Status: analysis.Unknown, Reads: []analysis.Read{
					{Kind: analysis.ReadHeader, Expr: "response.headers.x-id", Target: "x-id", Reader: "audit", Status: analysis.Unknown, Reason: "cannot tell from example v2-200.json"},
				}},
				{Kind: analysis.KindRequest, Collection: "identity", Status: analysis.Compatible},
			}},
	}}
	var b strings.Builder

	writeImpactText(&b, rep)

	want := `auth/login · identity
  version 1 · contract: none
    no consumers
  version 2 · contract: example v2-200.json
    ✗ [onboarding] flow signup › node login
        ✗ nodes.login.token → $.token (read by after): not in example v2-200.json
        ✗ input tenant (with.tenant): nothing here fills it
    ⚠ hook post · collection identity · inline #0
        ⚠ response.headers.x-id → x-id (read by audit): cannot tell from example v2-200.json
    ✓ request extract + assert
1 broken, 1 unknown, 1 compatible
`
	if b.String() != want {
		t.Fatalf("got:\n%s\nwant:\n%s", b.String(), want)
	}
}

func TestContractName_should_name_each_source(t *testing.T) {
	cases := []struct {
		c    analysis.ContractInfo
		want string
	}{
		{analysis.ContractInfo{}, "none"},
		{analysis.ContractInfo{Source: analysis.SourceResponse}, "last saved response"},
		{analysis.ContractInfo{Source: analysis.SourceExample, Files: []string{"/a/200.json"}}, "example 200.json"},
		{analysis.ContractInfo{Source: analysis.SourceSchema, Files: []string{"/a/1-200.json", "/a/1-404.json"}}, "schema 1-200.json, 1-404.json"},
	}
	for _, tc := range cases {
		if got := contractName(tc.c); got != tc.want {
			t.Errorf("contractName(%+v) = %q, want %q", tc.c, got, tc.want)
		}
	}
}
