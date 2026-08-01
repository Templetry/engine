// Package render executes a Plan against a virtual file tree. Disk is
// touched only when the tree is finally written, which buys dry-run,
// trivial golden tests and atomic output. Pure except the final write.
package render
