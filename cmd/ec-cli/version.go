package main

import "fmt"

// runVersion prints binary metadata. Matches the JSON-friendly shape
// the rest of the cluster's tooling expects.
func runVersion(deps appDeps) int {
	fmt.Fprintf(deps.stdout, "ec-cli\n")
	fmt.Fprintf(deps.stdout, "  version    %s\n", version)
	fmt.Fprintf(deps.stdout, "  commit     %s\n", commit)
	fmt.Fprintf(deps.stdout, "  build_time %s\n", buildTime)
	return 0
}
