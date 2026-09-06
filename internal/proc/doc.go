// Package proc gives a child process a process group of its own, so that a
// cancellation reaches whatever the child started — a signing helper waiting
// on a passphrase outlives a kill aimed at the child alone.
package proc
