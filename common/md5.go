package common

import (
    "crypto/md5"
    "encoding/hex"
    _ "fmt"
    "io"
    "os"
)

/*
    Code taken from Mr.Waggel's blog
    http://www.mrwaggel.be/post/generate-md5-hash-of-a-file-in-golang/
*/

//func hash_file_md5(filePath string) (string, error) {
func HashFile(filePath string) (string, error) {
    var returnMD5String string
    file, err := os.Open(filePath)
    if err != nil {
        return returnMD5String, err
    }
    defer file.Close()
    hash := md5.New()
    if _, err := io.Copy(hash, file); err != nil {
        return returnMD5String, err
    }
    hashInBytes := hash.Sum(nil)[:16]
    returnMD5String = hex.EncodeToString(hashInBytes)
    return returnMD5String, nil

}

/*
func main() {
    hash, err := hash_file_md5(os.Args[0])
    if err == nil {
        fmt.Println(hash)
    }
}
*/
