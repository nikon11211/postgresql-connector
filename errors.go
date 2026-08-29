package pgconnector

import "errors"

var (
	ErrNilConfig     = errors.New("pgconnector: config cannot be nil")
	ErrInvalidConfig = errors.New("pgconnector: invalid config")
	ErrNoMasterFound = errors.New("pgconnector: no master node found in cluster")
	ErrNotConnected  = errors.New("pgconnector: not connected to any database")
)
