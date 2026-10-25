package sign

// EmbeddedPublicKey is the production minisign public key (the base64 "RW..."
// line) compiled into the tool. A package list verifies when its signature
// matches this key or a key the user has added to the trusted-keys file. The
// secret half never enters a repository; see the maintainer guide.
const EmbeddedPublicKey = "RWQi6SYmWWH5aTsczFB+gN8bE20b+GzberGg7ZsRF/1oNxtBCRSlAkjF"
