package sign

// EmbeddedPublicKey is the production minisign public key (the base64 "RW..."
// line) compiled into the tool. It is empty until the maintainers fill in the
// production key before the first release; no production key is generated or
// committed during development. While it is empty, only keys the user has
// added to the trusted-keys file can verify an index.
const EmbeddedPublicKey = ""
