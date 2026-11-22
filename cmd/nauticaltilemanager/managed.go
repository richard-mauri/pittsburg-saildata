package main

import (
 "errors"
 "fmt"
 "io/fs"
 "os"
 "path/filepath"
 "strconv"
 "strings"
)

// managedInventory measures the WHOLE existing directory, not just the
// requested geographic rectangle. Caps are cumulative across acquisitions.
type managedInventory struct {
 Count int64
 Bytes int64
 Overlap int64
 New int64
}

func inspectManaged(o options,p plan) (string,managedInventory,error) {
 var inv managedInventory
 if strings.TrimSpace(o.Dir)=="" {
  if o.Download{return "",inv,errors.New("-dir is required with -download")}
  inv.New=p.Total
  return "",inv,nil
 }
 root,err:=filepath.Abs(o.Dir);if err!=nil{return "",inv,err}
 if root==string(filepath.Separator){return "",inv,errors.New("refusing filesystem root as tile output")}
 info,err:=os.Lstat(root)
 if os.IsNotExist(err) {
  inv.New=p.Total
  return root,inv,nil
 }
 if err!=nil{return "",inv,err}
 if !info.IsDir()||info.Mode()&os.ModeSymlink!=0{return "",inv,errors.New("managed output must be a real directory")}
 marker,err:=os.ReadFile(filepath.Join(root,markerFile))
 if err!=nil{return "",inv,errors.New("existing directory lacks a Tile Manager marker; refusing to modify unrelated data")}
 // Legacy v1 markers included original bounds and zoom. Retain those
 // unchanged for existing datasets while permitting new bounding boxes.
 // Source identity MUST stay fixed: never mix fictitious and NOAA charts.
 lines:=string(marker)
 if !strings.HasPrefix(lines,"SailingGo-managed-XYZ-v1\n")||
  !strings.Contains(lines,"\nsource="+o.Source+"\n"){
  return "",inv,errors.New("managed directory marker has a different source or unknown format")
 }
 // Reject unexpected files/links so an untracked dataset or symlink cannot
 // silently evade cumulative limits. Do not follow any symlinks.
 err=filepath.WalkDir(root,func(path string,d fs.DirEntry,walkErr error)error{
  if walkErr!=nil{return walkErr}
  if path==root{return nil}
  relative,err:=filepath.Rel(root,path);if err!=nil{return err}
  if d.Type()&os.ModeSymlink!=0{return fmt.Errorf("symlink not allowed in managed tile tree: %s",relative)}
  if relative==markerFile {
   if !d.Type().IsRegular(){return errors.New("managed marker is not a regular file")}
   return nil
  }
  parts:=strings.Split(relative,string(filepath.Separator))
  if d.IsDir(){
   if len(parts)==1{
    z,err:=strconv.Atoi(parts[0]);if err==nil&&z>=9&&z<=16{return nil}
   }
   if len(parts)==2 {
    z,e1:=strconv.Atoi(parts[0]);x,e2:=strconv.Atoi(parts[1])
    if e1==nil&&e2==nil&&z>=9&&z<=16&&x>=0&&x<1<<uint(z){return nil}
   }
   return fmt.Errorf("unexpected directory in managed tile tree: %s",relative)
  }
  if !d.Type().IsRegular(){return fmt.Errorf("nonregular tile entry: %s",relative)}
  if len(parts)!=3||!strings.HasSuffix(parts[2],".png"){
   return fmt.Errorf("unexpected file in managed tile tree: %s",relative)
  }
  z,e1:=strconv.Atoi(parts[0]);x,e2:=strconv.Atoi(parts[1])
  y,e3:=strconv.Atoi(strings.TrimSuffix(parts[2],".png"))
  if e1!=nil||e2!=nil||e3!=nil||z<9||z>16||x<0||x>=1<<uint(z)||y<0||y>=1<<uint(z) {
   return fmt.Errorf("invalid XYZ filename: %s",relative)
  }
  stat,err:=d.Info();if err!=nil{return err}
  if stat.Size()<=0||stat.Size()>maxPNGBytes{return fmt.Errorf("invalid PNG file size: %s",relative)}
  inv.Count++;inv.Bytes+=stat.Size()
  return nil
 })
 if err!=nil{return "",inv,err}
 if inv.Count>int64(o.MaxTiles){return "",inv,fmt.Errorf("existing managed dataset has %d tiles, exceeds cumulative -max-tiles=%d",inv.Count,o.MaxTiles)}
 if o.MaxBytes>0&&inv.Bytes>o.MaxBytes{return "",inv,fmt.Errorf("existing managed dataset has %d bytes, exceeds -max-bytes=%d",inv.Bytes,o.MaxBytes)}
 for _,row:=range p.Rows{
  for x:=row.X0;x<=row.X1;x++{for y:=row.Y0;y<=row.Y1;y++{
   path:=filepath.Join(root,strconv.Itoa(row.Z),strconv.Itoa(x),strconv.Itoa(y)+".png")
   info,err:=os.Lstat(path)
   if os.IsNotExist(err){inv.New++;continue}
   if err!=nil{return "",inv,err}
   if !info.Mode().IsRegular(){return "",inv,fmt.Errorf("nonregular requested tile: %s",path)}
   inv.Overlap++
  }}
 }
 if inv.Count+inv.New>int64(o.MaxTiles){
  return "",inv,fmt.Errorf("cumulative tiles %d existing + %d new = %d exceeds -max-tiles=%d; choose a smaller area or explicitly raise total cap",inv.Count,inv.New,inv.Count+inv.New,o.MaxTiles)
 }
 return root,inv,nil
}

func createManagedRoot(root string,o options)error{
 if root==""{return errors.New("managed output directory missing")}
 if info,err:=os.Lstat(root);err==nil{
  if !info.IsDir()||info.Mode()&os.ModeSymlink!=0{return errors.New("tile directory changed during preflight")}
  return nil
 }else if !os.IsNotExist(err){return err}
 // Create a marked directory, never silently adopt an existing unmarked one.
 if err:=os.Mkdir(root,0700);err!=nil{return err}
 if err:=os.WriteFile(filepath.Join(root,markerFile),[]byte(o.signature()),0600);err!=nil{return err}
 return nil
}
