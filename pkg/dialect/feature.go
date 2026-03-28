package dialect

// Feature describes a SQL capability supported by a dialect.
type Feature uint64

const (
	FeatureInsertReturning Feature = 1 << iota
	FeatureUpdateReturning
	FeatureDeleteReturning
	FeatureOffset
	FeatureUpsert
	FeatureCTE
	FeatureDefaultPlaceholder
)

// HasFeature reports whether a feature set includes the requested capability.
func HasFeature(features, feature Feature) bool {
	return feature != 0 && features&feature == feature
}

// HasAnyFeature reports whether a feature set includes any requested capability.
func HasAnyFeature(features, feature Feature) bool {
	return features&feature != 0
}
