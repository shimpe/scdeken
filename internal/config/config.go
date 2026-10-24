// Package config holds the constants that depend on where sc-deken is hosted.
// Renaming the GitHub organization is a find-and-replace in this file.
package config

const (
	// HelperName is the name of the Go helper executable.
	HelperName = "scdeken"
	// ToolRepo is the GitHub repository of the tool itself.
	ToolRepo = "shimpe/scdeken"
	// RecipesRepo is the GitHub repository holding recipes and advisories.
	RecipesRepo = "shimpe/scdeken-recipes"
	// DefaultIndexURL is where the package index is downloaded from.
	DefaultIndexURL = "https://shimpe.github.io/scdeken-recipes/index.json"
	// SCRepoURL is the SuperCollider source repository.
	SCRepoURL = "https://github.com/supercollider/supercollider"
	// QuarksDirectoryURL is the official list of quarks the indexer reads.
	QuarksDirectoryURL = "https://raw.githubusercontent.com/supercollider-quarks/quarks/master/directory.txt"
)
