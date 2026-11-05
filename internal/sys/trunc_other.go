//go:build !unix

package sys

// MsgTrunc is zero where truncation is not reported through flags.
const MsgTrunc = 0
