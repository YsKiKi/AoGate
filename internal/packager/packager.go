package packager

import (
	"archive/zip"
	"bufio"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// platformSpec 描述一个目标平台的打包规格
type platformSpec struct {
	label  string   // 显示名称
	key    string   // -platform 参数値
	zipExe string   // ZIP 包内可执行文件名
	dirs   []string // 本地可执行文件搜索路径（按优先级排列）
}

var supportedPlatforms = []platformSpec{
	{
		label:  "Windows x64",
		key:    "windows-amd64",
		zipExe: "launcher.exe",
		dirs:   []string{".", "build/windows", "build/windows/amd64"},
	},
	{
		label:  "Windows ARM64",
		key:    "windows-arm64",
		zipExe: "launcher.exe",
		dirs:   []string{".", "build/windows/arm64"},
	},
	{
		label:  "Linux x64",
		key:    "linux-amd64",
		zipExe: "launcher",
		dirs:   []string{".", "build/linux", "build/linux/amd64"},
	},
	{
		label:  "Linux ARM64",
		key:    "linux-arm64",
		zipExe: "launcher",
		dirs:   []string{".", "build/linux/arm64"},
	},
	{
		label:  "macOS x64",
		key:    "darwin-amd64",
		zipExe: "launcher",
		dirs:   []string{".", "build/darwin", "build/darwin/amd64"},
	},
	{
		label:  "macOS ARM64 (Apple Silicon)",
		key:    "darwin-arm64",
		zipExe: "launcher",
		dirs:   []string{".", "build/darwin/arm64"},
	},
}

// findExeForPlatform 在平台指定的目录中搜索可执行文件
func findExeForPlatform(spec platformSpec) string {
	// 构建搜索路径：可执行文件自身目录 + 平台定义目录
	searchDirs := spec.dirs
	if exePath, err := os.Executable(); err == nil {
		exeDir := filepath.Dir(exePath)
		searchDirs = append([]string{exeDir}, searchDirs...)
	}

	for _, dir := range searchDirs {
		p := filepath.Join(dir, spec.zipExe)
		if _, err := os.Stat(p); err == nil {
			return p
		}
		// 对 Linux/macOS 兼容旧命名约定（如 launcher_linux）
		if spec.zipExe == "launcher" {
			alt := filepath.Join(dir, "launcher_linux")
			if _, err := os.Stat(alt); err == nil {
				return alt
			}
		}
	}
	return ""
}

func Run(args []string) {
	fs := flag.NewFlagSet("packager", flag.ExitOnError)
	platformFlag := fs.String("platform", "", "目标平台 (如 windows-amd64，留空交互选择)")
	launcherFlag := fs.String("launcher", "", "指定启动器可执行文件路径 (留空自动搜索)")
	fs.Usage = func() {
		printUsage()
	}
	fs.Parse(args)

	fmt.Println("=== 网关启动器打包工具 ===")
	reader := bufio.NewReader(os.Stdin)

	// 1. 确定目标平台
	var spec platformSpec
	if *platformFlag != "" {
		found := false
		for _, p := range supportedPlatforms {
			if p.key == *platformFlag {
				spec = p
				found = true
				break
			}
		}
		if !found {
			fmt.Printf("错误：不支持的平台 '%s'。\n", *platformFlag)
			printUsage()
			pause()
			return
		}
	} else {
		// 交互式选择平台
		fmt.Println("\n支持的目标平台：")
		for i, p := range supportedPlatforms {
			fmt.Printf("[%d] %-26s %s\n", i+1, p.key, p.label)
		}
		fmt.Print("\n选择目标平台（输入数字）：")
		platInput, _ := reader.ReadString('\n')
		platInput = strings.TrimSpace(platInput)
		platIdx, err := strconv.Atoi(platInput)
		if err != nil || platIdx < 1 || platIdx > len(supportedPlatforms) {
			fmt.Println("无效选择。")
			pause()
			return
		}
		spec = supportedPlatforms[platIdx-1]
	}
	fmt.Printf("目标平台：%s (%s)\n", spec.label, spec.key)

	// 2. 查找可执行文件
	var exePath string
	if *launcherFlag != "" {
		if _, err := os.Stat(*launcherFlag); err != nil {
			fmt.Printf("错误：指定的启动器路径无效：%s\n", *launcherFlag)
			pause()
			return
		}
		exePath = *launcherFlag
	} else {
		exePath = findExeForPlatform(spec)
	}
	if exePath == "" {
		fmt.Printf("错误：未找到 %s 平台的可执行文件。\n", spec.key)
		fmt.Printf("搜索路径：%s\n", strings.Join(spec.dirs, ", "))
		fmt.Println("请先构建对应平台的项目，或使用 -launcher 指定路径。")
		pause()
		return
	}
	fmt.Printf("已找到可执行文件：%s\n", exePath)

	// 3. 扫描密鐥
	entries, err := os.ReadDir("keys")
	if err != nil {
		fmt.Println("错误：读取 'keys' 目录失败。请确保该目录存在。")
		pause()
		return
	}

	var keyFiles []string
	for _, entry := range entries {
		if !entry.IsDir() && strings.HasSuffix(entry.Name(), ".key") {
			keyFiles = append(keyFiles, entry.Name())
		}
	}

	if len(keyFiles) == 0 {
		fmt.Println("未在 'keys' 目录找到私钥文件 (.key)。")
		fmt.Println("请先运行 keygen 生成密钥。")
		pause()
		return
	}

	// 4. 用户选择密钥
	fmt.Println("\n可用密钥：")
	fmt.Println("[0] ** 打包所有密钥（批量模式） **")
	for i, name := range keyFiles {
		fmt.Printf("[%d] %s\n", i+1, name)
	}

	fmt.Print("\n选择密钥（输入数字，'0' 为全部，或用逗号分隔如 '1,3'）：")
	keyInput, _ := reader.ReadString('\n')
	keyInput = strings.TrimSpace(keyInput)

	var targets []string

	if keyInput == "0" || strings.ToLower(keyInput) == "all" {
		targets = keyFiles
		fmt.Println("已选择模式：所有密钥")
	} else {
		parts := strings.Split(keyInput, ",")
		for _, p := range parts {
			idx, err := strconv.Atoi(strings.TrimSpace(p))
			if err != nil || idx < 1 || idx > len(keyFiles) {
				continue
			}
			targets = append(targets, keyFiles[idx-1])
		}
	}

	if len(targets) == 0 {
		fmt.Println("无效选择或未选择任何密钥。")
		pause()
		return
	}

	fmt.Printf("已选择 %d 个密钥进行打包。\n", len(targets))

	// 5. 配置服务器地址
	fmt.Print("\n输入网关服务器地址（如 1.2.3.4:9999）：")
	serverAddr, _ := reader.ReadString('\n')
	serverAddr = strings.TrimSpace(serverAddr)
	if serverAddr == "" {
		fmt.Println("服务器地址不能为空。")
		pause()
		return
	}

	// 6. 打包
	fmt.Printf("即将为服务器打包：%s\n", serverAddr)
	fmt.Print("按回车键开始打包...")
	reader.ReadString('\n')

	configContent := fmt.Sprintf("server_addr=%s\n", serverAddr)
	successCount := 0

	for _, keyFile := range targets {
		playerName := strings.TrimSuffix(keyFile, ".key")
		outputZip := fmt.Sprintf("Launcher_%s_%s.zip", playerName, spec.key)

		fmt.Printf("正在打包 [%s] -> %s ... ", playerName, outputZip)
		if err := createZip(outputZip, exePath, spec.zipExe, "keys/"+keyFile, configContent); err != nil {
			fmt.Printf("错误：%v\n", err)
		} else {
			fmt.Println("完成")
			successCount++
		}
	}

	fmt.Println("\n-------------------------------------------")
	fmt.Printf("批量完成：已创建 %d/%d 个压缩包。\n", successCount, len(targets))
	fmt.Println("请将这些压缩包发送给对应的玩家。")
	fmt.Println("-------------------------------------------")
}

func createZip(zipFilename, localExePath, zipExeName, keyPath, configContent string) error {
	newZipFile, err := os.Create(zipFilename)
	if err != nil {
		return err
	}
	defer newZipFile.Close()

	zipWriter := zip.NewWriter(newZipFile)
	defer zipWriter.Close()

	// Add executable
	if err := addToZip(zipWriter, localExePath, zipExeName); err != nil {
		return err
	}

	// Add private.key (Renamed)
	if err := addToZip(zipWriter, keyPath, "keys/private.key"); err != nil {
		return err
	}

	// Add config.ini
	if err := addContentToZip(zipWriter, "config.ini", []byte(configContent)); err != nil {
		return err
	}

	// Add warning file
	if err := addContentToZip(zipWriter, "请勿将这些内容分享给他人，您可能因此受到影响", []byte{}); err != nil {
		return err
	}

	return nil
}

func addContentToZip(zipWriter *zip.Writer, destPath string, content []byte) error {
	w, err := zipWriter.Create(destPath)
	if err != nil {
		return err
	}
	_, err = w.Write(content)
	return err
}

func addToZip(zipWriter *zip.Writer, srcPath, destPath string) error {
	file, err := os.Open(srcPath)
	if err != nil {
		return err
	}
	defer file.Close()

	w, err := zipWriter.Create(destPath)
	if err != nil {
		return err
	}

	_, err = io.Copy(w, file)
	return err
}

func pause() {
	fmt.Println("\n按回车键退出...")
	bufio.NewReader(os.Stdin).ReadString('\n')
}

func printUsage() {
	fmt.Println("用法：packager [-platform <平台>] [-launcher <路径>]")
	fmt.Println("用于将启动器与密钥打包的交互式工具。")
	fmt.Println("选项：")
	fmt.Println("  -platform    目标平台 (留空交互选择)")
	fmt.Println("  -launcher    指定启动器可执行文件路径 (留空自动搜索)")
	fmt.Println("  -h, -help    显示此帮助信息")
	fmt.Println("\n支持的平台：")
	for _, p := range supportedPlatforms {
		fmt.Printf("  %-26s %s\n", p.key, p.label)
	}
}
