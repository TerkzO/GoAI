package image

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"image"
	"os"
	"path/filepath"
	"sync"

	ort "github.com/yalue/onnxruntime_go"
	"golang.org/x/image/draw"
)

/*
	性能优化：
	● 预分配张量：避免每次推理动态分配内存。
	● 单例环境：ONNX环境全局初始化一次。
	● 并发安全：结构体不含共享状态，支持多实例并发使用。
	● GPU支持：ONNX Runtime可配置GPU加速，提升推理速度。

	接口
	● PredictFromFile：从文件路径识别图像。
	● PredictFromBuffer：从字节缓冲区识别图像。
	● PredictFromImage：核心识别方法，处理 image.Image 对象。
	● Close：释放 ONNX 资源。
*/

// 封装完整的图像识别逻辑，包含ONNX会话、输入输出张量、标签列表和尺寸参数。
type ImageRecognizer struct {
	session      *ort.Session[float32] // ONNX推理会话
	inputName    string                // 输入张量名称（默认"data"）
	outputName   string                // 输出张量名称（默认"mobilenetv20_output_flatten0_reshape0"）
	inputH       int                   // 输入图像高度（默认224）
	inputW       int                   // 输入图像宽度（默认224）
	labels       []string              // ImageNet类别标签列表
	inputTensor  *ort.Tensor[float32]  // 预分配输入张量（1x3xHxW）
	outputTensor *ort.Tensor[float32]  // 预分配输出张量（1x1000）s
}

const (
	defaultInputName  = "data"
	defaultOutputName = "mobilenetv20_output_flatten0_reshape0"
)

var (
	initOnce sync.Once
	initErr  error
)

// NewImageRecognizer 创建识别器(默认使用 input/output 名称)
// 创建实例，传入模型路径、标签路径和尺寸。
// 函数内部初始化ONNX环境，创建输入输出张量，加载模型到会话，读取标签文件。失败时返回错误，确保资源正确释放。
func NewImageRecognizer(modelPath, labelPath string, inputH, inputW int) (*ImageRecognizer, error) {
	if inputH <= 0 || inputW <= 0 {
		inputH, inputW = 224, 224
	}

	// 初始化全局ONNX环境
	initOnce.Do(func() {
		initErr = ort.InitializeEnvironment()
	})
	if initErr != nil {
		return nil, fmt.Errorf("onnxruntime initilize error: %w", initErr)
	}

	// 预创建输入输出 Tensor
	inputShape := ort.NewShape(1, 3, int64(inputH), int64(inputW))
	inData := make([]float32, inputShape.FlattenedSize())
	inTensor, err := ort.NewTensor(inputShape, inData)
	if err != nil {
		return nil, fmt.Errorf("create input tensor failed: %w", err)
	}

	outShape := ort.NewShape(1, 1000)
	outTensor, err := ort.NewEmptyTensor[float32](outShape)
	if err != nil {
		inTensor.Destroy()
		return nil, fmt.Errorf("create output tensor failed: %w", err)
	}

	// 创建Session
	session, err := ort.NewSession[float32](
		modelPath,
		[]string{defaultInputName},
		[]string{defaultOutputName},
		[]*ort.Tensor[float32]{inTensor},
		[]*ort.Tensor[float32]{outTensor},
	)
	if err != nil {
		inTensor.Destroy()
		outTensor.Destroy()
		return nil, fmt.Errorf("create onnx session failed: %w", err)
	}

	// 读取 label 文件
	labels, err := loadLabels(labelPath)
	if err != nil {
		session.Destroy()
		inTensor.Destroy()
		outTensor.Destroy()
		return nil, err
	}

	return &ImageRecognizer{
		session:      session,
		inputName:    defaultInputName,
		outputName:   defaultOutputName,
		inputH:       inputH,
		inputW:       inputW,
		labels:       labels,
		inputTensor:  inTensor,
		outputTensor: outTensor,
	}, err
}

func (r *ImageRecognizer) Close() {
	if r.session != nil {
		_ = r.session.Destroy()
		r.session = nil
	}
	if r.inputTensor != nil {
		_ = r.inputTensor.Destroy()
		r.inputTensor = nil
	}
	if r.outputTensor != nil {
		_ = r.outputTensor.Destroy()
		r.outputTensor = nil
	}
}

func (r *ImageRecognizer) PredictFromFile(imagePath string) (string, error) {
	file, err := os.Open(filepath.Clean(imagePath))
	if err != nil {
		return "", fmt.Errorf("image not found: %w", err)
	}
	defer file.Close()

	img, _, err := image.Decode(file)
	if err != nil {
		return "", fmt.Errorf("failed to decode image: %w", err)
	}

	return r.PredictFromImage(img)
}

// 接收图像数据。首先解码为image.Image（支持JPEG/PNG/GIF），然后缩放到指定尺寸（使用CatmullRom插值）。
// 转换为NCHW格式float32数组：归一化像素值（0-255到0-1），排列为[R通道, G通道, B通道]。
func (r *ImageRecognizer) PredictFromBuffer(buf []byte) (string, error) {
	img, _, err := image.Decode(bytes.NewReader(buf))
	if err != nil {
		return "", fmt.Errorf("failed to decode image from buffer: %w", err)
	}
	return r.PredictFromImage(img)
}

func (r *ImageRecognizer) PredictFromImage(img image.Image) (string, error) {

	resizedImg := image.NewRGBA(image.Rect(0, 0, r.inputW, r.inputH))

	draw.CatmullRom.Scale(resizedImg, resizedImg.Bounds(), img, img.Bounds(), draw.Over, nil)

	h, w := r.inputH, r.inputW
	ch := 3 // R, G, B
	data := make([]float32, h*w*ch)

	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			c := resizedImg.At(x, y)

			r, g, b, _ := c.RGBA()

			rf := float32(r>>8) / 255.0
			gf := float32(g>>8) / 255.0
			bf := float32(b>>8) / 255.0

			// NCHW format
			data[y*w+x] = rf
			data[h*w+y*w+x] = gf
			data[2*h*w+y*w+x] = bf
		}
	}

	// 将预处理数据复制到inputTensor，调用session.Run()执行ONNX推理。输出张量包含1000个类别的概率值。

	inData := r.inputTensor.GetData()
	copy(inData, data)

	if err := r.session.Run(); err != nil {
		return "", fmt.Errorf("onnx run error: %w", err)
	}
	// 遍历输出数组找到最大概率索引，映射到labels切片返回类别名称。若索引无效返回"Unknown"。
	outData := r.outputTensor.GetData()
	if len(outData) == 0 {
		return "", errors.New("empty output from model")
	}

	maxIdx := 0
	maxVal := outData[0]
	for i := 1; i < len(outData); i++ {
		if outData[i] > maxVal {
			maxVal = outData[i]
			maxIdx = i
		}
	}

	if maxIdx >= 0 && maxIdx < len(r.labels) {
		return r.labels[maxIdx], nil
	}
	return "Unknown", nil
}

func loadLabels(path string) ([]string, error) {
	f, err := os.Open(filepath.Clean(path))
	if err != nil {
		return nil, fmt.Errorf("open label file failed: %w", err)
	}
	// Close方法销毁会话和张量，释放ONNX资源。defer确保异常情况下资源释放
	defer f.Close()

	var labels []string
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := sc.Text()
		if line != "" {
			labels = append(labels, line)
		}
	}
	if err := sc.Err(); err != nil {
		return nil, fmt.Errorf("read labels failed: %w", err)
	}
	if len(labels) == 0 {
		return nil, fmt.Errorf("no labels found in %s", path)
	}
	return labels, nil
}
