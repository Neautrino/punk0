package main

import "strings"

// restartArgv is the command line Restart now runs, from the one punk0 was
// started with (args = os.Args, nArg = flag.NArg()): its flags, then
// -restarted, -no-open and the port it is bound to, last so they win over the
// user's, then the same folder or file. A "--" that ended the flags stays in
// front of the positional arguments, and one is added when the first starts
// with a dash. The flags an earlier restart added are replaced, not repeated.
func restartArgv(args []string, nArg int, port string) []string {
	flags, rest := args[1:len(args)-nArg], args[len(args)-nArg:]
	dashes := len(flags) > 0 && flags[len(flags)-1] == "--"
	if dashes {
		flags = flags[:len(flags)-1]
	}
	if n := len(flags); n >= 4 && flags[n-4] == "-restarted" && flags[n-3] == "-no-open" && flags[n-2] == "-port" {
		flags = flags[:n-4]
	}
	argv := append([]string{args[0]}, flags...)
	argv = append(argv, "-restarted", "-no-open", "-port", port)
	if dashes || len(rest) > 0 && strings.HasPrefix(rest[0], "-") {
		argv = append(argv, "--")
	}
	return append(argv, rest...)
}
