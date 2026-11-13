//go:build !linux && !darwin

package socket

func apply(_ uintptr, _ string, o Options, _ bool) error {
	switch {
	case o.Interface != "":
		return optionError("Interface", nil)
	case o.Mark != nil:
		return optionError("Mark", nil)
	case o.ReusePort:
		return optionError("ReusePort", nil)
	default:
		return nil
	}
}
