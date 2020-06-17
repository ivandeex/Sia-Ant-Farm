module gitlab.com/NebulousLabs/Sia-Ant-Farm

go 1.13

replace github.com/xtaci/smux => ../Sia/vendor/github.com/xtaci/smux

require (
	github.com/julienschmidt/httprouter v1.3.0
	gitlab.com/NebulousLabs/Sia v1.4.11
	gitlab.com/NebulousLabs/fastrand v0.0.0-20181126182046-603482d69e40
	gitlab.com/NebulousLabs/go-upnp v0.0.0-20181011194642-3a71999ed0d3
	gitlab.com/NebulousLabs/merkletree v0.0.0-20200118113624-07fbf710afc4
)
