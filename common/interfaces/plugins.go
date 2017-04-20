package interfaces

// IManagerPlugin is the interface we are exposing as a plugin. It is
// not directly a manager interface, as we have to handle goroutines
// in the plugin
type IManagerController interface {
	// Manager functions extended
	RetrieveDBStateByHeight(height uint32) error
	UploadDBStateBytes(data []byte, sign bool) error

	// Control function
	IsBufferEmpty() bool
	FetchFromBuffer() []byte
	SetSigningKey(sec []byte) error
}

// IEtcdManager plugin interface
type IEtcdManager interface {
	SendIntoEtcd(msg []byte) error
	GetData(int64) ([]byte, int64)

	// Ready will return true when the etcd client is instantiaed. It will return
	// an error if the plugin process is unreachable
	Ready() (bool, error)
}
