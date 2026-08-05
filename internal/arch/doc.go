// Package arch hosts the architecture-enforcement test. It has no runtime code;
// arch_test.go asserts the package dependency rules documented in the project
// README so an accidental layering violation fails the build instead of
// silently rotting the structure as the codebase grows.
package arch
