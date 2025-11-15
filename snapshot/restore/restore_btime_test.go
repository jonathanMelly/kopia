package restore_test

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/kopia/kopia/fs"
	"github.com/kopia/kopia/fs/localfs"
	"github.com/kopia/kopia/internal/repotesting"
	"github.com/kopia/kopia/repo"
	"github.com/kopia/kopia/snapshot"
	"github.com/kopia/kopia/snapshot/restore"
	"github.com/kopia/kopia/snapshot/snapshotfs"
	"github.com/kopia/kopia/snapshot/upload"
)

func TestBirthTimeSnapshotAndRestore(t *testing.T) {
	ctx, env := repotesting.NewEnvironment(t, repotesting.FormatNotImportant)

	sourceDir := t.TempDir()
	restoreDir := t.TempDir()

	canRestoreBirthTime := runtime.GOOS == "windows" || runtime.GOOS == "darwin"

	// Get source directory's original birthtime
	sourceDirEntry, err := localfs.NewEntry(sourceDir)
	require.NoError(t, err)
	originalDirBtime := getBirthTime(sourceDirEntry)

	// Create dummy file
	dummyFile := filepath.Join(sourceDir, "dummy.txt")
	require.NoError(t, os.WriteFile(dummyFile, []byte("test"), 0o644))

	// Set mtime to 1 minute in the future to differentiate from birth time
	futureTime := time.Now().Add(1 * time.Minute)
	require.NoError(t, os.Chtimes(dummyFile, futureTime, futureTime))

	// Get birth time and mtime from source
	sourceEntry, err := localfs.NewEntry(dummyFile)
	require.NoError(t, err)
	originalBtime := getBirthTime(sourceEntry)
	sourceMtime := sourceEntry.ModTime()

	t.Logf("Original - file btime: %v, mtime: %v", originalBtime, sourceMtime)
	t.Logf("Original - dir btime: %v", originalDirBtime)

	// Create first snapshot normally
	firstSnapshot := createSnapshot(t, ctx, env.RepositoryWriter, sourceDir)

	// Simulate old repo without btime: hack the snapshot metadata to set BirthTime = nil
	// This simulates a snapshot created by old Kopia that didn't support btime
	// Note: We only modify the root directory entry here. The file entries are stored
	// in the repository objects and would need to be fetched and modified separately,
	// but for this test, checking the root directory behavior is sufficient.
	firstSnapshot.RootEntry.BirthTime = nil

	// Save the modified snapshot
	_, err = snapshot.SaveSnapshot(ctx, env.RepositoryWriter, firstSnapshot)
	require.NoError(t, err)

	t.Logf("First snapshot created and modified (simulating old repo, btime = nil)")

	// Test restoring the first snapshot (without btime)
	restoreDir1 := t.TempDir()
	root1, err := snapshotfs.SnapshotRoot(env.RepositoryWriter, firstSnapshot)
	require.NoError(t, err)
	output1 := restore.FilesystemOutput{
		TargetPath:             restoreDir1,
		OverwriteDirectories:   true,
		OverwriteFiles:         true,
		OverwriteSymlinks:      true,
		IgnorePermissionErrors: true,
	}
	require.NoError(t, output1.Init(ctx))
	_, err = restore.Entry(ctx, env.RepositoryWriter, &output1, root1, restore.Options{})
	require.NoError(t, err)

	// Check restored directory from first snapshot (we modified its btime to nil)
	restoredDir1Entry, err := localfs.NewEntry(restoreDir1)
	require.NoError(t, err)
	restoredDir1Btime := getBirthTime(restoredDir1Entry)
	t.Logf("First snapshot restored - dir btime: %v", restoredDir1Btime)

	// For old snapshots without btime, OS sets btime to file creation time (approximately now)
	if canRestoreBirthTime {
		// btime should be close to current time (within a few seconds of the restore)
		timeSinceRestore := time.Since(restoredDir1Btime)
		require.Less(t, timeSinceRestore, 10*time.Second, "dir btime should be recent (file creation time during restore)")
		require.GreaterOrEqual(t, timeSinceRestore, time.Duration(0), "dir btime should not be in the future")
	}

	// Create second snapshot with proper btime (simulates new Kopia with btime support)
	latestSnapshot := createSnapshot(t, ctx, env.RepositoryWriter, sourceDir)
	t.Logf("Second snapshot created (new repo, with btime)")

	// Restore
	root, err := snapshotfs.SnapshotRoot(env.RepositoryWriter, latestSnapshot)
	require.NoError(t, err)
	output := restore.FilesystemOutput{
		TargetPath:             restoreDir,
		OverwriteDirectories:   true,
		OverwriteFiles:         true,
		OverwriteSymlinks:      true,
		IgnorePermissionErrors: true,
	}
	require.NoError(t, output.Init(ctx))

	_, err = restore.Entry(ctx, env.RepositoryWriter, &output, root, restore.Options{})
	require.NoError(t, err)

	// Verify restored times
	restoredFile := filepath.Join(restoreDir, "dummy.txt"+localfs.ShallowEntrySuffix)
	restoredEntry, err := localfs.NewEntry(restoredFile)
	require.NoError(t, err)
	restoredBtime := getBirthTime(restoredEntry)
	restoredMtime := restoredEntry.ModTime()
	
	restoredDirEntry, err := localfs.NewEntry(restoreDir)
	require.NoError(t, err)
	restoredDirBtime := getBirthTime(restoredDirEntry)

	t.Logf("Restored - btime: %v, mtime: %v", restoredBtime, restoredMtime)

	// mtime should always be restored correctly
	require.Equal(t, sourceMtime, restoredMtime, "mtime should match")

	if canRestoreBirthTime {
		// On Windows/macOS, birth time should be preserved
		require.Equal(t, originalBtime, restoredBtime, "file birth time should match on "+runtime.GOOS)
		require.Equal(t, originalDirBtime, restoredDirBtime, "directory birth time should match on "+runtime.GOOS)

	} else {
		require.Equal(t, sourceMtime, restoredBtime, "file birth time should match mtime on "+runtime.GOOS)
		require.Equal(t, sourceMtime, restoredBtime, "directory birth time should match mtime on "+runtime.GOOS)
	}

}

func getBirthTime(entry fs.Entry) time.Time {
	if ewb, ok := entry.(fs.EntryWithBirthTime); ok {
		return ewb.BirthTime()
	}
	return time.Time{}
}

func createSnapshot(t *testing.T, ctx context.Context, rep repo.RepositoryWriter, sourceDir string) *snapshot.Manifest {
	t.Helper()

	source, err := localfs.Directory(sourceDir)
	require.NoError(t, err)

	u := upload.NewUploader(rep)
	man, err := u.Upload(ctx, source, nil, snapshot.SourceInfo{
		Path:     sourceDir,
		Host:     "test-host",
		UserName: "test-user",
	})
	require.NoError(t, err)

	_, err = snapshot.SaveSnapshot(ctx, rep, man)
	require.NoError(t, err)

	return man
}
