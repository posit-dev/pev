package primitives

// Importing this package triggers each primitive's init() so it registers with
// the checks package's runtime registry. Other packages should
// `import _ "github.com/posit-dev/pev/internal/primitives"` to enable the
// catalog at runtime.
