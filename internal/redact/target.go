// Package redact locates and removes/replaces the PHI-bearing label image in
// a WSI file. Format-specific knowledge lives behind the Classifier interface;
// the byte-level write is delegated to internal/tifflow.
package redact

import "image"

// Role identifies the kind of associated image a Target refers to. Only
// RoleLabel is acted on today; the others exist so classifiers can report
// them (and future work can redact them) without a rearchitecture.
type Role int

const (
	RoleLabel Role = iota
	RoleOverview
	RoleThumbnail
)

func (r Role) String() string {
	switch r {
	case RoleLabel:
		return "label"
	case RoleOverview:
		return "overview"
	case RoleThumbnail:
		return "thumbnail"
	default:
		return "unknown"
	}
}

// Representation describes how a target image is physically stored, which
// selects the redaction strategy.
type Representation int

const (
	// SeparableIFD: the image is its own IFD with its own byte range; it can
	// be removed/replaced by a byte-exact splice.
	SeparableIFD Representation = iota
	// EmbeddedRegion: the image is a region of a larger associated image and
	// must be redacted by decode/blank/re-encode. (Not produced in Band 0.)
	EmbeddedRegion
)

// Target is one classified associated image.
type Target struct {
	Role           Role
	Representation Representation
	// IFDIndex is the chain index of the target IFD (SeparableIFD). It is
	// passed directly as tifflow.SpliceParams.TargetIdx.
	IFDIndex int
	// HostIFDIndex / Region describe an EmbeddedRegion (unused in Band 0).
	HostIFDIndex int
	Region       image.Rectangle
	// Width, Height are the target image's pixel dimensions.
	Width, Height int
}
