// Package planner turns (manifest, user inputs, FileSet) into a Plan:
// a serializable, printable list of operations. Pure — no filesystem,
// no network, no clock. The plan IS the dry-run output.
package planner
