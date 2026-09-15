package tex

// Invocation owns the stable Tectonic compile command shape. Filesystem
// locations remain inside process adapters; callers across product or RPC
// seams use project-relative semantic values instead.
type Invocation struct {
	MainFile        string
	OutputDirectory string
	Bundle          string
	Untrusted       bool
	OnlyCached      bool
}

// Arguments returns a fresh Tectonic 0.17 compile argument vector.
func (invocation Invocation) Arguments() []string {
	arguments := []string{"-X", "compile"}
	if invocation.Bundle != "" {
		arguments = append(arguments, "--bundle", invocation.Bundle)
	}
	if invocation.Untrusted {
		arguments = append(arguments, "--untrusted")
	}
	if invocation.OnlyCached {
		arguments = append(arguments, "--only-cached")
	}
	arguments = append(
		arguments,
		"--outdir", invocation.OutputDirectory,
		"--synctex",
		"--keep-logs",
		"--print",
		invocation.MainFile,
	)
	return arguments
}
