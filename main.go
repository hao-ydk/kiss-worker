// kiss-worker 的 Go 实现提供与 KISS-Translator 客户端兼容的数据同步服务。
//
// 对外协议只有两个入口：POST /sync 用于双向同步设置数据，GET /rules 用于读取
// 已分享的规则。客户端传入的 value 始终被视为不透明字符串；服务端只依据 updateAt
// 判断应返回远端数据还是保存客户端数据。持久化层使用 key 的 SHA-256 摘要作为文件名，
// 同时保留对旧版明文文件名的只读兼容，以避免升级时要求用户迁移数据。
package main

import (
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/gin-contrib/cors"
	"github.com/gin-gonic/gin"
)

const (
	// 下列环境变量和盐值属于既有客户端/部署协议，修改会导致现有实例无法鉴权或读取数据。
	ENV_APP_KEY        = "APP_KEY"
	ENV_DATA_PATH      = "APP_DATAPATH"
	DEFAULT_DATA_PATH  = "data"
	KV_SALT_SYNC       = "KISS-Translator-SYNC"
	KV_SALT_SHARE      = "KISS-Translator-SHARE"
	KV_RULES_SHARE_KEY = "kiss-rules-share.json"

	// key 长度与 Cloudflare KV 的限制保持一致，便于两套后端接受相同的客户端请求。
	maxKeyBytes = 512
	// JavaScript Number 能精确表达的最大整数，用于保证 Go 与 Worker 的时间戳语义一致。
	maxSafeInteger = int64(9007199254740991)
	// 小于该值的正整数视为旧版秒级 Unix 时间戳；正常的现代毫秒时间戳大于该值。
	legacySecondsCutoff  = int64(100000000000)
	storageFileExtension = ".json"
)

// config 保存启动后只读的服务配置。测试会直接替换这两个字段以隔离数据目录。
var config = struct {
	appKey  string
	dataDir string
}{}

// keyLocks 是固定大小的分片锁表。使用摘要首字节选锁可避免为任意用户 key
// 永久分配互斥锁，同时保证同一 key 的“读取、比较、写入”在进程内串行执行。
var keyLocks [256]sync.Mutex

// kvData 是磁盘格式及 /sync 成功响应共用的数据结构。
// Value 允许为空字符串，且可能包含普通 JSON 或客户端生成的加密 envelope。
type kvData struct {
	Key      string `json:"key"`
	Value    string `json:"value"`
	UpdateAt int64  `json:"updateAt"`
}

// syncRequest 使用指针区分“字段缺失”和字段存在但值为空/为零。
// 这是保持空字符串 value 与 updateAt=0 协议语义所必需的。
type syncRequest struct {
	Key      *string `json:"key"`
	Value    *string `json:"value"`
	UpdateAt *int64  `json:"updateAt"`
}

// normalizeUpdateAt 将旧 Go 服务生成的秒级时间戳升级为客户端使用的毫秒时间戳。
// 已经是毫秒或值为零时保持原样，避免重复转换和破坏首次同步标记。
func normalizeUpdateAt(updateAt int64) int64 {
	if updateAt > 0 && updateAt < legacySecondsCutoff {
		return updateAt * 1000
	}
	return updateAt
}

// storagePath 将不可信的业务 key 映射为数据目录内的固定文件名。
// 文件路径中不包含原始 key，因此即使 key 带有 ../ 或路径分隔符也无法目录穿越。
func storagePath(key string) string {
	sum := sha256.Sum256([]byte(key))
	return filepath.Join(config.dataDir, hex.EncodeToString(sum[:])+storageFileExtension)
}

// legacyStoragePath 为升级前以原始 key 命名的文件生成兼容路径。
// 只接受单个安全文件名，不接受绝对路径、目录分隔符或特殊目录名；该路径只用于读取。
func legacyStoragePath(key string) (string, bool) {
	if key == "" || key == "." || filepath.IsAbs(key) || strings.ContainsAny(key, `/\\`) || filepath.Base(key) != key {
		return "", false
	}
	return filepath.Join(config.dataDir, key), true
}

// lockForKey 为同一业务 key 稳定选择一个分片锁。
func lockForKey(key string) *sync.Mutex {
	sum := sha256.Sum256([]byte(key))
	return &keyLocks[sum[0]]
}

// decodeData 解码磁盘记录，并在读取边界统一旧版时间单位。
// JSON 损坏必须显式返回错误，不能把零值结构当成有效远端数据覆盖客户端。
func decodeData(data []byte) (*kvData, error) {
	var kv kvData
	if err := json.Unmarshal(data, &kv); err != nil {
		return nil, fmt.Errorf("decode data: %w", err)
	}
	kv.UpdateAt = normalizeUpdateAt(kv.UpdateAt)
	return &kv, nil
}

// loadDataUnlocked 读取当前安全文件；不存在时再尝试安全的旧版文件名。
// 调用方必须持有 lockForKey(key) 返回的锁，以免与原子替换过程交错。
func loadDataUnlocked(key string) (*kvData, error) {
	if err := checkDirExist(config.dataDir); err != nil {
		return nil, err
	}

	// 新格式始终优先。一旦 key 写入过新格式，就不再回退到可能陈旧的旧文件。
	data, err := os.ReadFile(storagePath(key))
	if err == nil {
		return decodeData(data)
	}
	if !os.IsNotExist(err) {
		return nil, err
	}

	// 回退仅用于无损升级；不安全的旧 key 不会被拼接到文件系统路径中。
	legacyPath, ok := legacyStoragePath(key)
	if !ok {
		return nil, err
	}
	data, err = os.ReadFile(legacyPath)
	if err != nil {
		return nil, err
	}
	return decodeData(data)
}

// saveDataUnlocked 将完整记录写入临时文件，刷盘并原子替换目标文件。
// 调用方必须持有对应 key 的锁。临时文件与目标位于同一目录，确保重命名不会跨文件系统；
// 任何中途错误都会保留旧文件，defer 则负责清理未完成的临时文件。
func saveDataUnlocked(kv *kvData) error {
	if err := checkDirExist(config.dataDir); err != nil {
		return err
	}

	data, err := json.MarshalIndent(kv, "", "  ")
	if err != nil {
		return fmt.Errorf("encode data: %w", err)
	}

	tmp, err := os.CreateTemp(config.dataDir, ".kiss-worker-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)

	if err := tmp.Chmod(0600); err != nil {
		tmp.Close()
		return err
	}
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	// 先确保内容进入底层存储，再关闭并重命名，避免成功响应对应半写文件。
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmpName, storagePath(kv.Key)); err != nil {
		return err
	}
	return nil
}

// checkDirExist 确保数据路径存在且确实是目录。
// 分开处理 MkdirAll、Stat 和类型检查，避免旧实现遇到权限错误时解引用空 FileInfo。
func checkDirExist(dir string) error {
	if err := os.MkdirAll(dir, 0700); err != nil {
		return err
	}
	stat, err := os.Stat(dir)
	if err != nil {
		return err
	}
	if !stat.IsDir() {
		return fmt.Errorf("data path is not a directory: %s", dir)
	}
	return nil
}

// calSha256 实现客户端约定的 SHA256(text + salt) 十六进制摘要。
// 该函数用于保持既有协议兼容，不应替换盐值或调整拼接顺序。
func calSha256(text string, salt string) string {
	h := sha256.New()
	h.Write([]byte(text))
	h.Write([]byte(salt))
	return hex.EncodeToString(h.Sum(nil))
}

// credentialsMatch 使用常量时间比较鉴权摘要，避免普通字符串比较暴露逐字节时序差异。
func credentialsMatch(actual, expected string) bool {
	return subtle.ConstantTimeCompare([]byte(actual), []byte(expected)) == 1
}

// validateSyncRequest 校验两套后端共同支持的请求边界，并转换为持久化结构。
// value 只要求是字符串且允许为空；服务端不会解析其中的业务数据或加密格式。
func validateSyncRequest(req syncRequest) (*kvData, error) {
	if req.Key == nil || req.Value == nil || req.UpdateAt == nil {
		return nil, errors.New("missing required field")
	}
	if *req.Key == "" || len([]byte(*req.Key)) > maxKeyBytes {
		return nil, errors.New("invalid key")
	}
	if *req.UpdateAt < 0 || *req.UpdateAt > maxSafeInteger {
		return nil, errors.New("invalid updateAt")
	}
	return &kvData{Key: *req.Key, Value: *req.Value, UpdateAt: *req.UpdateAt}, nil
}

// handleSync 处理现有客户端的 POST /sync 协议。
// 鉴权、字段名称和成功响应保持不变；锁覆盖整个读取、版本比较和写入流程，确保并发请求
// 不会让较旧 updateAt 覆盖较新记录。updateAt=0 仅在远端不存在时生成当前毫秒时间。
func handleSync(c *gin.Context) {
	expectPsk := fmt.Sprintf("Bearer %s", calSha256(config.appKey, KV_SALT_SYNC))
	if !credentialsMatch(c.GetHeader("Authorization"), expectPsk) {
		log.Printf("sync authentication failed")
		c.JSON(400, gin.H{"message": "invalid key."})
		return
	}

	var body syncRequest
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(400, gin.H{"message": "req bind err"})
		return
	}
	req, err := validateSyncRequest(body)
	if err != nil {
		c.JSON(400, gin.H{"message": "req bind err"})
		return
	}

	// 同一分片内的不同 key 可能短暂互斥，这是用有界内存换取安全并发的设计取舍。
	keyLock := lockForKey(req.Key)
	keyLock.Lock()
	defer keyLock.Unlock()

	res, err := loadDataUnlocked(req.Key)
	if err != nil && !os.IsNotExist(err) {
		log.Printf("load data failed: %s", err)
		c.JSON(500, gin.H{"message": "load data err"})
		return
	}
	// 相同时间戳也以远端为准，这是客户端现有的冲突解决规则。
	if err == nil && res.UpdateAt >= req.UpdateAt {
		c.JSON(200, res)
		return
	}

	if req.UpdateAt == 0 {
		req.UpdateAt = time.Now().UnixMilli()
	}
	if err := saveDataUnlocked(req); err != nil {
		log.Printf("save data failed: %s", err)
		c.JSON(500, gin.H{"message": "save data err"})
		return
	}
	c.JSON(200, req)
}

// handleRules 处理 GET /rules?psk=...，返回已分享规则的原始 JSON。
// psk 继续放在查询参数中是客户端兼容要求；安全日志中只记录 URL.Path，不记录查询串。
func handleRules(c *gin.Context) {
	expectPsk := calSha256(config.appKey, KV_SALT_SHARE)
	if !credentialsMatch(c.Query("psk"), expectPsk) {
		log.Printf("rules authentication failed")
		c.JSON(400, gin.H{"message": "invalid key"})
		return
	}

	keyLock := lockForKey(KV_RULES_SHARE_KEY)
	keyLock.Lock()
	res, err := loadDataUnlocked(KV_RULES_SHARE_KEY)
	keyLock.Unlock()
	if os.IsNotExist(err) {
		c.JSON(404, gin.H{"message": "not found"})
		return
	}
	if err != nil {
		log.Printf("load rules failed: %s", err)
		c.JSON(500, gin.H{"message": "load data err"})
		return
	}
	c.Data(200, "application/json; charset=utf-8", []byte(res.Value))
}

// requestLogger 记录不含 RawQuery 的请求路径，防止 /rules 的 psk 被写入应用日志。
func requestLogger() gin.HandlerFunc {
	return func(c *gin.Context) {
		started := time.Now()
		c.Next()
		log.Printf("%s %s %d %s", c.Request.Method, c.Request.URL.Path, c.Writer.Status(), time.Since(started))
	}
}

// newRouter 组装 HTTP 中间件和兼容路由。
// CORS 允许任意来源是浏览器扩展和用户脚本跨站调用同步服务所需的既有行为。
func newRouter() *gin.Engine {
	r := gin.New()
	r.Use(requestLogger(), gin.Recovery())
	corsConf := cors.DefaultConfig()
	corsConf.AllowOrigins = []string{"*"}
	corsConf.AllowHeaders = []string{"*"}
	r.Use(cors.New(corsConf))
	r.POST("/sync", handleSync)
	r.GET("/rules", handleRules)
	return r
}

// loadConfig 从环境变量加载启动配置。
// APP_KEY 不提供公开默认值；相对数据路径以当前工作目录为基准，绝对路径则保持原意。
func loadConfig() error {
	appKey := os.Getenv(ENV_APP_KEY)
	if appKey == "" {
		return fmt.Errorf("%s environment variable is required", ENV_APP_KEY)
	}
	rootDir, err := os.Getwd()
	if err != nil {
		return err
	}
	dataPath := os.Getenv(ENV_DATA_PATH)
	if dataPath == "" {
		dataPath = DEFAULT_DATA_PATH
	}
	if !filepath.IsAbs(dataPath) {
		dataPath = filepath.Join(rootDir, dataPath)
	}
	config.appKey = appKey
	config.dataDir = filepath.Clean(dataPath)
	return nil
}

// main 完成配置校验并启动 Gin。任何配置或监听错误都直接终止进程，避免服务以未知状态运行。
func main() {
	log.SetPrefix("[KISS] ")
	if err := loadConfig(); err != nil {
		log.Fatal(err)
	}
	if err := newRouter().Run(); err != nil {
		log.Fatal(err)
	}
}
