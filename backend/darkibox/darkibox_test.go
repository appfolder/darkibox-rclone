// Test Darkibox filesystem interface
package darkibox_test

import (
	"testing"

	"github.com/rclone/rclone/backend/darkibox"
	"github.com/rclone/rclone/fstest"
	"github.com/rclone/rclone/fstest/fstests"
)

// TestIntegration runs integration tests against the remote
func TestIntegration(t *testing.T) {
	if *fstest.RemoteName == "" {
		*fstest.RemoteName = "TestDarkibox:"
	}
	fstests.Run(t, &fstests.Opt{
		RemoteName: *fstest.RemoteName,
		NilObject:  (*darkibox.Object)(nil),
	})
}
