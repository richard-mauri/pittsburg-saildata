package main

import (
 "errors"
 "fmt"
 "io/fs"
 "os"
 "path/filepath"
 "sort"
 "strconv"
 "strings"
)

// pruneManaged retains only native NOAA zooms 13 and 14. The default is a
// non-mutating inventory: -apply-prune is required to delete any file.
// Remove individual files and then empty directories, never RemoveAll.
// This prevents newly added unrelated entries from being removed recursively.
func pruneManaged(o options, apply bool) error {
 if o.Source!="noaa" {return errors.New("pruning is restricted to manager-marked NOAA datasets")}
 if strings.TrimSpace(o.Dir)=="" {return errors.New("-dir is required for NOAA prune")}
 // This operation must not be blocked by the old all-zoom count/byte budgets:
 // their purpose is to limit acquisition, not prevent cleanup.
 check:=o
 check.MaxTiles=int(^uint(0)>>1)
 check.MaxBytes=0
 root,inv,err:=inspectManaged(check,plan{})
 if err!=nil{return fmt.Errorf("unsafe managed directory, no files removed: %w",err)}
 if root=="" {return errors.New("prune requires an existing managed directory")}
 if _,err:=os.Stat(filepath.Join(root,markerFile));err!=nil{return errors.New("managed directory marker missing")}
 var paths,dirs []string
 var count,bytes int64
 for z:=9;z<=16;z++ {
  if z==13||z==14{continue}
  zoomDir:=filepath.Join(root,strconv.Itoa(z))
  if _,err:=os.Lstat(zoomDir);os.IsNotExist(err){continue}else if err!=nil{return err}
  err=filepath.WalkDir(zoomDir,func(path string,d fs.DirEntry,walkErr error)error{
   if walkErr!=nil{return walkErr}
   if d.Type()&os.ModeSymlink!=0 {return fmt.Errorf("symlink in managed cache: %s",path)}
   if d.IsDir(){dirs=append(dirs,path);return nil}
   if !d.Type().IsRegular() {return fmt.Errorf("nonregular file in managed cache: %s",path)}
   stat,err:=d.Info();if err!=nil{return err}
   count++;bytes+=stat.Size()
   paths=append(paths,path)
   return nil
  })
  if err!=nil{return err}
 }
 if count>inv.Count||bytes>inv.Bytes{return errors.New("prune inventory disagrees with total manager inventory")}
 fmt.Printf("NOAA cleanup: keep native z13..14; delete %d PNG tiles (%d PNG bytes); retain %d tiles (%d PNG bytes)\n",
  count,bytes,inv.Count-count,inv.Bytes-bytes)
 if !apply {
  fmt.Println("DRY RUN: no files removed; pass -prune -apply-prune to delete only z9..12 and z15..16")
  return nil
 }
 // Re-check at the deletion boundary so a new path cannot be silently removed.
 _,again,err:=inspectManaged(check,plan{})
 if err!=nil||again.Count!=inv.Count||again.Bytes!=inv.Bytes {
  return errors.New("managed dataset changed or is unsafe since pruning inventory; nothing removed")
 }
 for _,path:=range paths {
  stat,err:=os.Lstat(path)
  if err!=nil||!stat.Mode().IsRegular(){return fmt.Errorf("file changed before prune: %s: %v",path,err)}
  if err:=os.Remove(path);err!=nil{return fmt.Errorf("prune tile %s: %w",path,err)}
 }
 sort.Slice(dirs,func(i,j int)bool{return len(dirs[i])>len(dirs[j])})
 for _,path:=range dirs {
  // os.Remove only succeeds on empty directories, unlike RemoveAll.
  if err:=os.Remove(path);err!=nil{return fmt.Errorf("empty old zoom directory %s: %w",path,err)}
 }
 fmt.Printf("Pruned %d unused NOAA tiles; retained native z13..14; marker unchanged\n",count)
 return nil
}
