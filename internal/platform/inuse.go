package platform

// InUse reports whether a video application is currently using a camera.
//
// The Insta360 Link 2 parks its gimbal while nothing streams, so position
// changes only hold while this is true. Windows reports camera use per app
// (not per device); macOS reports it per device.
func InUse() (bool, error) { return inUse() }
