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

	t.Logf("Original - btime: %v, mtime: %v", originalBtime, sourceMtime)

	// Simulate old repo without btime: set btime to zero (simulating no btime info)
	if canRestoreBirthTime {
		err = restore.ChtimesExact(dummyFile, time.Time{}, sourceMtime, sourceMtime)
		require.NoError(t, err)
	}

	// Verify btime is now set to zero/epoch
	entryBeforeSnap1, err := localfs.NewEntry(dummyFile)
	require.NoError(t, err)
	btimeBeforeSnap1 := getBirthTime(entryBeforeSnap1)
	t.Logf("Before snapshot 1 - btime: %v (should be 0/epoch)", btimeBeforeSnap1)

	// Create first snapshot (simulates old snapshot without btime metadata)
	_ = createSnapshot(t, ctx, env.RepositoryWriter, sourceDir)
	t.Logf("First snapshot created (old repo, btime = 0)")

	// Restore original birth time to simulate migration scenario
	if canRestoreBirthTime {
		err = restore.ChtimesExact(dummyFile, originalBtime, sourceMtime, sourceMtime)
		require.NoError(t, err)
	}

	// Verify btime is restored
	entryBeforeSnap2, err := localfs.NewEntry(dummyFile)
	require.NoError(t, err)
	btimeBeforeSnap2 := getBirthTime(entryBeforeSnap2)
	t.Logf("Before snapshot 2 - btime: %v (btime restored)", btimeBeforeSnap2)

	// Create second snapshot (should capture the proper btime due to migration logic)
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
		require.Equal(t, originalBtime, restoredDirBtime, "directory birth time should match on "+runtime.GOOS)

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
