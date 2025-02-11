package main

import (
	"flag"
	"fmt"
	"io"
	"log/slog"
	"math"
	"os"
	"path/filepath"
	"strings"

	"github.com/hajimehoshi/ebiten/audio"
	"github.com/hajimehoshi/ebiten/v2/audio/wav"
	"gonum.org/v1/gonum/dsp/fourier"
)

// DecodeSQ decodes an SQ encoded stereo channels into quadriphonic channels.
// LT and RT are the left-total and right-total input signals.
// alpha is normally 1/SQR(2).
// Returns the decoded back-left and back-right signals.

func DecodeSQ(LT []float64, RT []float64) ([]float64, []float64, []float64, []float64) {
	// Decode with alpha = 1/SQR(2) (adjust as needed)
	var alpha float64 = 1 / math.Sqrt(2)
	log.Info("DecodeSQ...")

	N := len(LT)
	if len(RT) != N {
		log.Error("Input slices LT and RT must have the same length")
		panic("Input slices LT and RT must have the same length")
	}
	// Init fourier transform
	fft := fourier.NewFFT(N)

	// Compute frequency domain signals
	freqLT := fft.Coefficients(nil, LT)
	freqRT := fft.Coefficients(nil, RT)

	M := len(freqLT)
	log.Info("Expected FFT output size:", "expected", (N/2)+1, "actual", M)
	// fmt.Printf("Expected FFT output size: %d, actual: %d\n", (N/2)+1, M)

	// Init slices for front channels and back channels
	frontLeft := make([]complex128, M)
	frontRight := make([]complex128, M)
	backLeft := make([]complex128, M)
	backRight := make([]complex128, M)

	// for i := range M {
	for i := 0; i < M; i++ {
		// lf = LT
		frontLeft[i] = freqLT[i]
		// rf = RT
		frontRight[i] = freqRT[i]
		// Compute back left channel: lb = -alpha * (RT - j*LT)
		backLeft[i] = complex(-alpha, 0) * (freqRT[i] - complex(0, 1)*freqLT[i])
		// Compute back right channel: rb = alpha * (LT - j*RT)
		backRight[i] = complex(alpha, 0) * (freqLT[i] - complex(0, 1)*freqRT[i])
	}

	// Inverse FFT to go back to time domain
	frontLeftTime := fft.Sequence(nil, frontLeft)
	frontRightTime := fft.Sequence(nil, frontRight)
	backLeftTime := fft.Sequence(nil, backLeft)
	backRightTime := fft.Sequence(nil, backRight)

	// Normalize
	normalize(&backLeftTime, &backRightTime)
	normalize(&frontLeftTime, &frontRightTime)

	log.Info("DecodeSQ is done.")

	return frontLeftTime, frontRightTime, backLeftTime, backRightTime
}

func normalize(left *[]float64, back *[]float64) {
	maxVal := math.Max(maxAbs(*left), maxAbs(*back))
	if maxVal > 1 {
		for i := range *left {
			(*left)[i] /= maxVal
			(*back)[i] /= maxVal
		}
	}
}

// Helper function to find max absolute value for normalization
func maxAbs(data []float64) float64 {
	max := 0.0
	for _, v := range data {
		abs := math.Abs(v)
		if abs > max {
			max = abs
		}
	}
	return max
}

// createWAVHeader crée un en-tête WAV pour le nombre donné d'échantillons.
func createWAVHeader(sampleRate int) []byte {

	// Write WAV header manually
	// >> is used to perform right bit shift
	// sampleRate >> 8 shifts the bits 8 positions to the right, effectively dividing by 256 (2^8) and getting the second byte of the number.
	// sampleRate >> 16 shifts 16 positions, giving the third byte.
	// sampleRate >> 24 for the fourth byte.
	// The & 0xFF operation masks out all but the least significant byte after the shift, ensuring only one byte is written.

	return []byte{
		'R', 'I', 'F', 'F', 0, 0, 0, 0, // RIFF
		'W', 'A', 'V', 'E', // WAVE
		'f', 'm', 't', ' ', 16, 0, 0, 0, // fmt
		1, 0, // Compression code (1 = PCM)
		2, 0, // Number of channels
		byte(sampleRate & 0xFF), byte((sampleRate >> 8) & 0xFF), byte((sampleRate >> 16) & 0xFF), byte((sampleRate >> 24) & 0xFF), // Sample rate
		byte((sampleRate * 4) & 0xFF), byte(((sampleRate * 4) >> 8) & 0xFF), byte(((sampleRate * 4) >> 16) & 0xFF), byte(((sampleRate * 4) >> 24) & 0xFF), // Byte rate (sampleRate * channels * bitsPerSample / 8)
		4, 0, // Block align
		16, 0, // Bits per sample
		'd', 'a', 't', 'a', 0, 0, 0, 0, // data
	}
}

func readWaveFile(s string) ([]float64, []float64, int, error) {
	f, err := os.Open(s)
	if err != nil {
		return nil, nil, 0, fmt.Errorf("error opening WAV file: %w", err)
	}
	defer f.Close()

	context, err := audio.NewContext(44100) // Assuming 44.1 kHz; adjust if necessary
	if err != nil {
		return nil, nil, 0, fmt.Errorf("failed to create audio context: %w", err)
	}

	d, err := wav.DecodeWithSampleRate(context.SampleRate(), f)
	if err != nil {
		return nil, nil, 0, fmt.Errorf("error decoding WAV: %w", err)
	}

	var sampleRate int = context.SampleRate()
	err = nil

	data, err := io.ReadAll(d)
	if err != nil {
		return nil, nil, 0, fmt.Errorf("error reading WAV data: %w", err)
	}

	// Assuming stereo, 16-bit samples:
	// 4 bytes per stereo  (2 bytes per channel)
	expectedSamples := len(data) / 4

	// Convert byte data to float64 for processing
	LT := make([]float64, len(data)/4)
	RT := make([]float64, len(data)/4)

	for i := 0; i < len(data)/4; i++ {
		LT[i] = float64(int16(data[i*4+0])|int16(data[i*4+1])<<8) / float64(math.MaxInt16)
		RT[i] = float64(int16(data[i*4+2])|int16(data[i*4+3])<<8) / float64(math.MaxInt16)
	}

	// Check lengths before decoding
	log.Info("Wave Data Input in bytes (2 bytes per channel) ", "InputLength", len(data), "expectedSample", expectedSamples, "LeftChannelLength", len(LT), "RightChannelLength", len(RT))

	return LT, RT, sampleRate, nil
}

func writeWaveFile(s string, sampleRate int, left []float64, right []float64) error {
	// Write back channels to new WAV file
	outFile, err := os.Create(s)
	if err != nil {
		return fmt.Errorf("error creating  WAV file: %w", err)

	}
	defer outFile.Close()

	header := createWAVHeader(sampleRate)

	// Write the header to the file
	outFile.Write(header)

	// Write audio data
	// Each audio signal  is converted from float to a 16-bit integer, then split into two bytes
	// & 0xFF gets the least significant byte.
	// >> 8 shifts to get the next byte for the 16-bit integer representation.

	for i := 0; i < len(left); i++ {
		outFile.Write([]byte{
			byte(int16(left[i]*float64(math.MaxInt16)) & 0xFF),
			byte(int16(left[i]*float64(math.MaxInt16)) >> 8),
			byte(int16(right[i]*float64(math.MaxInt16)) & 0xFF),
			byte(int16(right[i]*float64(math.MaxInt16)) >> 8),
		})
	}

	// Update header sizes
	outSize := int64(len(left) * 4) // Each sample is 4 bytes (2 channels * 2 bytes per sample)
	chunkSize := 36 + outSize       // 36 = size of the header up to data chunk
	header[4] = byte(chunkSize & 0xFF)
	header[5] = byte((chunkSize >> 8) & 0xFF)
	header[6] = byte((chunkSize >> 16) & 0xFF)
	header[7] = byte((chunkSize >> 24) & 0xFF)
	header[40] = byte(outSize & 0xFF)
	header[41] = byte((outSize >> 8) & 0xFF)
	header[42] = byte((outSize >> 16) & 0xFF)
	header[43] = byte((outSize >> 24) & 0xFF)

	// Write the updated header back to the file
	outFile.Seek(0, 0)
	outFile.Write(header)

	return nil
}

func fileNameExtract(file string) string {
	fileName := filepath.Base(file)
	nameWithoutExt := strings.TrimSuffix(fileName, filepath.Ext(fileName))
	return nameWithoutExt
}

func printHelp() {
	fmt.Println("2025 : See my blog https://jeandi7.github.io/jeandi7blog/")
	fmt.Println()
	fmt.Println("Usage: sqdecoder [options]")
	fmt.Println("Options:")
	flag.PrintDefaults()
	os.Exit(0)
}

func InitLogger() *slog.Logger {
	return slog.New(slog.NewJSONHandler(os.Stdout, nil))
}

var log = InitLogger()

func main() {
	var input string = ""
	var showHelp bool

	flag.StringVar(&input, "input", "", "Read audio Wave File")
	flag.BoolVar(&showHelp, "help", false, "Show help message")
	flag.Parse()

	if input == "" {
		fmt.Println("you must provide an input audio wave file name.")
		printHelp()
		return
	}

	if showHelp {
		printHelp()
		return
	}

	LT, RT, sampleRate, err := readWaveFile(input)
	if err != nil {
		log.Error("Failed to read:", "input", input, "error", err)
		return
	}

	frontLeft, frontRight, backLeft, backRight := DecodeSQ(LT, RT)

	filename := fileNameExtract(input)
	filenameBackChanels := "output_back_" + filename + ".wav"
	filenameFrontChanels := "output_front_" + filename + ".wav"

	log.Info("Write output back channels...", "ouput", filenameBackChanels)

	err = writeWaveFile(filenameBackChanels, sampleRate, backLeft, backRight)
	if err != nil {
		log.Error("Failed to write output back channels:", "error", err)
		return
	}

	log.Info("Write output front channels...", "ouput", filenameFrontChanels)
	err = writeWaveFile(filenameFrontChanels, sampleRate, frontLeft, frontRight)
	if err != nil {
		log.Error("Failed to write output front chanels:", "error", err)
		return
	}

}
