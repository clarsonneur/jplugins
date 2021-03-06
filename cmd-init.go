package main

import (
	"github.com/alecthomas/kingpin"
)

type cmdInit struct {
	cmd              *kingpin.CmdClause
	lockfile         cmdInitLockfile
	features         cmdInitFeatures
}

func (c *cmdInit) init() {
	c.cmd = App.app.Command("init", "Initialize files to use jplugins.")
	c.lockfile.init(c.cmd)
	c.features.init(c.cmd)
}
