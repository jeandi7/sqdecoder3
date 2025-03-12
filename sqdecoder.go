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

// used for QS to 5.1
func DecodeQSTo5_1(LT []float64, RT []float64) ([]float64, []float64, []float64, []float64, []float64, []float64) {
	var alpha float64 = 0.924
	var beta float64 = 0.383
	var alpha1 float64 = 1 / math.Sqrt(2)

	// lfecoeff : -10db = 0.316...
	var lfecoeff = math.Pow(10, -1.0/2.0)
	log.Info("DecodeQS to 5.1 (experimental) ...", "lfcoeff", lfecoeff)

	N := len(LT)
	if len(RT) != N {
		log.Error("Input slices LT and RT must have the same length : QS decoding")
		panic("Input slices LT and RT must have the same length in QS decoding")
	}

	fft := fourier.NewFFT(N)
	freqLT := fft.Coefficients(nil, LT)
	freqRT := fft.Coefficients(nil, RT)

	M := len(freqLT)
	log.Info("Expected FFT output size in QS decoding:", "expected", (N/2)+1, "actual", M)

	frontLeft := make([]complex128, M)
	frontRight := make([]complex128, M)
	backLeft := make([]complex128, M)
	backRight := make([]complex128, M)
	center := make([]complex128, M)
	lfe := make([]complex128, M)

	for i := 0; i < M; i++ {
		// lf = 0.924*LT + 0.383*RT
		frontLeft[i] = complex(alpha, 0)*freqLT[i] + complex(beta, 0)*freqRT[i]
		// rf = 0.383*LT+0.924*RT
		frontRight[i] = complex(beta, 0)*freqRT[i] + complex(alpha, 0)*freqRT[i]
		// Compute back left channel: lb = j * (0.383*RT - 0.924*LT)
		backLeft[i] = complex(0, 1) * (complex(beta, 0)*freqRT[i] - complex(alpha, 0)*freqLT[i])
		// Compute back right channel: rb = j * (0.383*LT - 0.924*RT)
		backRight[i] = complex(0, 1) * (complex(beta, 0)*freqLT[i] - complex(alpha, 0)*freqRT[i])
		center[i] = complex(alpha1, 0) * (freqLT[i] + freqRT[i])
		lfe[i] = complex(lfecoeff, 0) * (freqLT[i] + freqRT[i] + backLeft[i] + backRight[i])
	}

	// Appliquer le filtre passe-bas à lfe

	// lowPassFilterLFE(lfe, 44100.0, 150.0) // Supposons 44.1 kHz, ajustez selon votre cas
	lowPassFilterLFEContinuous(lfe, 44100.0, 150.0)

	log.Info("QS decoding: lfe size:", "lfetime", len(lfe), "lfe", len(lfe))

	frontLeftTime := fft.Sequence(nil, frontLeft)
	frontRightTime := fft.Sequence(nil, frontRight)
	backLeftTime := fft.Sequence(nil, backLeft)
	backRightTime := fft.Sequence(nil, backRight)
	centerTime := fft.Sequence(nil, center)
	lfeTime := fft.Sequence(nil, lfe)

	normalize(&frontLeftTime, &frontRightTime)
	normalize(&backLeftTime, &backRightTime)
	// normalize(&centerTime, &lfeTime)

	normalizeSingle(&centerTime)
	normalizeSingle(&lfeTime)

	log.Info("DecodeQS to 5.1 is done.")

	return frontLeftTime, frontRightTime, centerTime, lfeTime, backLeftTime, backRightTime
}

// DecodeQS decodes an QS encoded stereo channels into quadriphonic channels.
// LT and RT are the left-total and right-total input signals.
// Returns the decoded back-left and back-right signals.

func DecodeQS(LT []float64, RT []float64) ([]float64, []float64, []float64, []float64) {
	var alpha float64 = 0.924
	var beta float64 = 0.383
	log.Info("DecodeQS...")

	N := len(LT)
	if len(RT) != N {
		log.Error("Input slices LT and RT must have the same length: QS decoding")
		panic("Input slices LT and RT must have the same length: QS decoding")
	}
	// Init fourier transform
	fft := fourier.NewFFT(N)

	// Compute frequency domain signals
	freqLT := fft.Coefficients(nil, LT)
	freqRT := fft.Coefficients(nil, RT)

	M := len(freqLT)
	log.Info("QS decoding : Expected FFT output size:", "expected", (N/2)+1, "actual", M)
	// fmt.Printf("Expected FFT output size: %d, actual: %d\n", (N/2)+1, M)

	// Init slices for front channels and back channels
	frontLeft := make([]complex128, M)
	frontRight := make([]complex128, M)
	backLeft := make([]complex128, M)
	backRight := make([]complex128, M)

	// for i := range M {
	for i := 0; i < M; i++ {
		// lf = 0.924*LT + 0.383*RT
		frontLeft[i] = complex(alpha, 0)*freqLT[i] + complex(beta, 0)*freqRT[i]
		// rf = 0.383*LT+0.924*RT
		frontRight[i] = complex(beta, 0)*freqRT[i] + complex(alpha, 0)*freqRT[i]
		// Compute back left channel: lb = j * (0.383*RT - 0.924*LT)
		backLeft[i] = complex(0, 1) * (complex(beta, 0)*freqRT[i] - complex(alpha, 0)*freqLT[i])
		// Compute back right channel: rb = j * (0.383*LT - 0.924*RT)
		backRight[i] = complex(0, 1) * (complex(beta, 0)*freqLT[i] - complex(alpha, 0)*freqRT[i])
	}

	// Inverse FFT to go back to time domain
	frontLeftTime := fft.Sequence(nil, frontLeft)
	frontRightTime := fft.Sequence(nil, frontRight)
	backLeftTime := fft.Sequence(nil, backLeft)
	backRightTime := fft.Sequence(nil, backRight)

	// Normalize
	normalize(&backLeftTime, &backRightTime)
	normalize(&frontLeftTime, &frontRightTime)

	log.Info("DecodeQS is done.")

	return frontLeftTime, frontRightTime, backLeftTime, backRightTime
}

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
		log.Error("Input slices LT and RT must have the same length : SQ decoding")
		panic("Input slices LT and RT must have the same length : SQ decoding")
	}
	// Init fourier transform
	fft := fourier.NewFFT(N)

	// Compute frequency domain signals
	freqLT := fft.Coefficients(nil, LT)
	freqRT := fft.Coefficients(nil, RT)

	M := len(freqLT)
	log.Info("SQ decoding : Expected FFT output size:", "expected", (N/2)+1, "actual", M)
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

func normalize(left *[]float64, right *[]float64) {
	maxVal := math.Max(maxAbs(*left), maxAbs(*right))
	if maxVal > 1 {
		for i := range *left {
			(*left)[i] /= maxVal
			(*right)[i] /= maxVal
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

func normalizeSingle(channel *[]float64) {
	maxVal := maxAbs(*channel)
	if maxVal > 1 {
		for i := range *channel {
			(*channel)[i] /= maxVal
		}
	}
}

// Filter based on a continuous function in order to reduce the oscillations caused by the approximation.
func lowPassFilterLFEContinuous(lfe []complex128, sampleRate float64, cutoffFreq float64) {
	M := len(lfe)    // Nombre de coefficients fréquentiels
	N := 2 * (M - 1) // Longueur du signal temporel
	freqResolution := sampleRate / float64(N)

	// Paramètre de pente pour la décroissance (plus tau est grand, plus la transition est douce)
	tau := 0.7 * cutoffFreq

	log.Info("Continuous lowPassFilterLFE : ", "freqResolution", freqResolution, "Nbr coeff freq ", M)

	// Appliquer une fonction continue dans le domaine fréquentiel
	for i := 0; i < M; i++ {
		// Fréquence correspondante à l'indice i
		freq := float64(i) * freqResolution

		// Fonction continue : atténuation exponentielle après cutoffFreq
		if freq > cutoffFreq {
			attenuation := math.Exp(-((freq - cutoffFreq) / tau))
			lfe[i] = lfe[i] * complex(attenuation, 0)
		}
		// Les fréquences <= cutoffFreq restent inchangées (gain de 1)
	}
}

// lowpassFilter with a rectangular window function H(f)
func lowPassFilterLFE(lfe []complex128, sampleRate float64, cutoffFreq float64) {
	M := len(lfe)    // Nombre de coefficients fréquentiels
	N := 2 * (M - 1) // Longueur du signal temporel
	freqResolution := sampleRate / float64(N)
	cutoffIndex := int(cutoffFreq/freqResolution) + 1

	log.Info("lowPassFilterLFE : ", "freqResolution", freqResolution, "cutOffIndex", cutoffIndex, "Nbr coeff freq ", M)

	for i := cutoffIndex; i < M; i++ {
		lfe[i] = complex(0, 0) // Removes positive high frequencies
	}
}

// SQ used for 5.1
func DecodeSQTo5_1(LT []float64, RT []float64) ([]float64, []float64, []float64, []float64, []float64, []float64) {
	var alpha float64 = 1 / math.Sqrt(2)

	// lfecoeff : -10db = 0.316...
	var lfecoeff = math.Pow(10, -1.0/2.0)
	log.Info("DecodeSQ to 5.1 (experimental) ...", "lfcoeff", lfecoeff)

	N := len(LT)
	if len(RT) != N {
		log.Error("SQ decoding : Input slices LT and RT must have the same length")
		panic("SQ decoding : Input slices LT and RT must have the same length")
	}

	fft := fourier.NewFFT(N)
	freqLT := fft.Coefficients(nil, LT)
	freqRT := fft.Coefficients(nil, RT)

	M := len(freqLT)
	log.Info("SQ decoding : Expected FFT output size:", "expected", (N/2)+1, "actual", M)

	frontLeft := make([]complex128, M)
	frontRight := make([]complex128, M)
	backLeft := make([]complex128, M)
	backRight := make([]complex128, M)
	center := make([]complex128, M)
	lfe := make([]complex128, M)

	for i := 0; i < M; i++ {
		frontLeft[i] = freqLT[i]
		frontRight[i] = freqRT[i]
		backLeft[i] = complex(-alpha, 0) * (freqRT[i] - complex(0, 1)*freqLT[i])
		backRight[i] = complex(alpha, 0) * (freqLT[i] - complex(0, 1)*freqRT[i])
		center[i] = complex(alpha, 0) * (freqLT[i] + freqRT[i])
		lfe[i] = complex(lfecoeff, 0) * (freqLT[i] + freqRT[i] + backLeft[i] + backRight[i])
	}

	// Appliquer le filtre passe-bas à lfe

	// lowPassFilterLFE(lfe, 44100.0, 150.0) // Supposons 44.1 kHz, ajustez selon votre cas
	lowPassFilterLFEContinuous(lfe, 44100.0, 150.0)

	log.Info("SQ decoding : lfe size:", "lfetime", len(lfe), "lfe", len(lfe))

	frontLeftTime := fft.Sequence(nil, frontLeft)
	frontRightTime := fft.Sequence(nil, frontRight)
	backLeftTime := fft.Sequence(nil, backLeft)
	backRightTime := fft.Sequence(nil, backRight)
	centerTime := fft.Sequence(nil, center)
	lfeTime := fft.Sequence(nil, lfe)

	normalize(&frontLeftTime, &frontRightTime)
	normalize(&backLeftTime, &backRightTime)
	// normalize(&centerTime, &lfeTime)

	normalizeSingle(&centerTime)
	normalizeSingle(&lfeTime)

	log.Info("DecodeSQ to 5.1 is done.")

	return frontLeftTime, frontRightTime, centerTime, lfeTime, backLeftTime, backRightTime
}

// used for 5.1
func createWAVHeader5_1(sampleRate int) []byte {
	// Write WAV header manually
	// >> is used to perform right bit shift
	// sampleRate >> 8 shifts the bits 8 positions to the right, effectively dividing by 256 (2^8) and getting the second byte of the number.
	// sampleRate >> 16 shifts 16 positions, giving the third byte.
	// sampleRate >> 24 for the fourth byte.
	// The & 0xFF operation masks out all but the least significant byte after the shift, ensuring only one byte is written.

	return []byte{
		'R', 'I', 'F', 'F', 0, 0, 0, 0, // RIFF (chunk ID, taille totale à mettre à jour plus tard)
		'W', 'A', 'V', 'E', // WAVE (format)
		'f', 'm', 't', ' ', 16, 0, 0, 0, // fmt (subchunk1 ID, subchunk1 size = 16 pour PCM)
		1, 0, // Compression code (1 = PCM)
		6, 0, // Number of channels (6 pour 5.1)
		byte(sampleRate & 0xFF), byte((sampleRate >> 8) & 0xFF), byte((sampleRate >> 16) & 0xFF), byte((sampleRate >> 24) & 0xFF), // Sample rate
		byte((sampleRate * 12) & 0xFF), byte(((sampleRate * 12) >> 8) & 0xFF), byte(((sampleRate * 12) >> 16) & 0xFF), byte(((sampleRate * 12) >> 24) & 0xFF), // Byte rate (sampleRate * channels * bitsPerSample / 8 = sampleRate * 6 * 16 / 8 = sampleRate * 12)
		12, 0, // Block align (channels * bitsPerSample / 8 = 6 * 16 / 8 = 12)
		16, 0, // Bits per sample (16 bits)
		'd', 'a', 't', 'a', 0, 0, 0, 0, // data (subchunk2 ID, taille des données à mettre à jour plus tard)
	}
}

// used for 5.1
func writeWaveFile5_1(s string, sampleRate int, leftFront, rightFront, leftBack, rightBack, center, lfe []float64) error {
	numSamples := len(leftFront)
	if len(rightFront) != numSamples || len(leftBack) != numSamples || len(rightBack) != numSamples || len(center) != numSamples || len(lfe) != numSamples {
		return fmt.Errorf("all channels must be the same length")
	}

	outFile, err := os.Create(s)
	if err != nil {
		return fmt.Errorf("error creating WAV file: %w", err)
	}
	defer outFile.Close()

	// Créer l'en-tête WAV pour 5.1 (6 canaux, 16 bits)
	header := createWAVHeader5_1(sampleRate)

	// Écrire l'en-tête initial dans le fichier
	_, err = outFile.Write(header)
	if err != nil {
		return fmt.Errorf("error writing WAV header: %w", err)
	}

	// write (6 canaux, 16 bits) dans l'ordre SMPTE : L, R, C, LFE, Ls, Rs
	for i := 0; i < numSamples; i++ {
		sampleL := int16(leftFront[i] * float64(math.MaxInt16))  // L
		sampleR := int16(rightFront[i] * float64(math.MaxInt16)) // R
		sampleC := int16(center[i] * float64(math.MaxInt16))     // C
		sampleLFE := int16(lfe[i] * float64(math.MaxInt16))      // LFE
		sampleLs := int16(leftBack[i] * float64(math.MaxInt16))  // Ls
		sampleRs := int16(rightBack[i] * float64(math.MaxInt16)) // Rs

		_, err := outFile.Write([]byte{
			byte(sampleL & 0xFF), byte(sampleL >> 8), // L
			byte(sampleR & 0xFF), byte(sampleR >> 8), // R
			byte(sampleC & 0xFF), byte(sampleC >> 8), // C
			byte(sampleLFE & 0xFF), byte(sampleLFE >> 8), // LFE
			byte(sampleLs & 0xFF), byte(sampleLs >> 8), // Ls
			byte(sampleRs & 0xFF), byte(sampleRs >> 8), // Rs
		})
		if err != nil {
			return fmt.Errorf("error writing audio data: %w", err)
		}
	}

	// Mettre à jour les tailles dans l'en-tête
	outSize := int64(numSamples * 6 * 2) // 6 canaux * 2 octets par échantillon
	chunkSize := 36 + outSize            // 36 = taille de l'en-tête jusqu'au chunk de données

	header[4] = byte(chunkSize & 0xFF)
	header[5] = byte((chunkSize >> 8) & 0xFF)
	header[6] = byte((chunkSize >> 16) & 0xFF)
	header[7] = byte((chunkSize >> 24) & 0xFF)
	header[40] = byte(outSize & 0xFF)
	header[41] = byte((outSize >> 8) & 0xFF)
	header[42] = byte((outSize >> 16) & 0xFF)
	header[43] = byte((outSize >> 24) & 0xFF)

	// Réécrire l'en-tête mis à jour
	_, err = outFile.Seek(0, 0)
	if err != nil {
		return fmt.Errorf("erreur lors du repositionnement au début du fichier : %w", err)
	}
	_, err = outFile.Write(header)
	if err != nil {
		return fmt.Errorf("erreur lors de la réécriture de l'en-tête : %w", err)
	}

	return nil
}

// Header file 4.0 (4 channels, 16 bits).
func createWAVHeader4_0(sampleRate int) []byte {
	// Write WAV header manually
	// >> is used to perform right bit shift
	// sampleRate >> 8 shifts the bits 8 positions to the right, effectively dividing by 256 (2^8) and getting the second byte of the number.
	// sampleRate >> 16 shifts 16 positions, giving the third byte.
	// sampleRate >> 24 for the fourth byte.
	// The & 0xFF operation masks out all but the least significant byte after the shift, ensuring only one byte is written.

	return []byte{
		'R', 'I', 'F', 'F', 0, 0, 0, 0, // RIFF (chunk ID, taille totale à mettre à jour plus tard)
		'W', 'A', 'V', 'E', // WAVE (format)
		'f', 'm', 't', ' ', 16, 0, 0, 0, // fmt (subchunk1 ID, subchunk1 size = 16 pour PCM)
		1, 0, // Compression code (1 = PCM)
		4, 0, // Number of channels (4 pour 4.0)
		byte(sampleRate & 0xFF), byte((sampleRate >> 8) & 0xFF), byte((sampleRate >> 16) & 0xFF), byte((sampleRate >> 24) & 0xFF), // Sample rate
		byte((sampleRate * 8) & 0xFF), byte(((sampleRate * 8) >> 8) & 0xFF), byte(((sampleRate * 8) >> 16) & 0xFF), byte(((sampleRate * 8) >> 24) & 0xFF), // Byte rate (sampleRate * channels * bitsPerSample / 8 = sampleRate * 4 * 16 / 8 = sampleRate * 8)
		8, 0, // Block align (channels * bitsPerSample / 8 = 4 * 16 / 8 = 8)
		16, 0, // Bits per sample (16 bits)
		'd', 'a', 't', 'a', 0, 0, 0, 0, // data (subchunk2 ID, taille des données à mettre à jour plus tard)
	}
}

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

// writeWaveFile4_0 écrit un fichier WAV au format 4.0 (quadraphonie).
func writeWaveFile4_0(s string, sampleRate int, leftFront, rightFront, leftBack, rightBack []float64) error {

	numSamples := len(leftFront)
	if len(rightFront) != numSamples || len(leftBack) != numSamples || len(rightBack) != numSamples {
		return fmt.Errorf("all channels must be the same length")
	}

	outFile, err := os.Create(s)
	if err != nil {
		return fmt.Errorf("error creating WAV file %w", err)
	}
	defer outFile.Close()

	// Créer l'en-tête WAV pour 4.0 (4 canaux, 16 bits)
	header := createWAVHeader4_0(sampleRate)

	// Écrire l'en-tête initial dans le fichier
	_, err = outFile.Write(header)
	if err != nil {
		return fmt.Errorf("error writing WAV header: %w", err)
	}
	//  write (4 channels, 16 bits)
	for i := 0; i < numSamples; i++ {
		sampleLF := int16(leftFront[i] * float64(math.MaxInt16))
		sampleRF := int16(rightFront[i] * float64(math.MaxInt16))
		sampleLB := int16(leftBack[i] * float64(math.MaxInt16))
		sampleRB := int16(rightBack[i] * float64(math.MaxInt16))

		_, err := outFile.Write([]byte{
			byte(sampleLF & 0xFF), byte(sampleLF >> 8), // LF
			byte(sampleRF & 0xFF), byte(sampleRF >> 8), // RF
			byte(sampleLB & 0xFF), byte(sampleLB >> 8), // LB
			byte(sampleRB & 0xFF), byte(sampleRB >> 8), // RB
		})
		if err != nil {
			return fmt.Errorf("error writing audio data : %w", err)
		}
	}

	outSize := int64(numSamples * 4 * 2) // 4 channels * 2 bytes per samples
	chunkSize := 36 + outSize            // 36 = size of the header up to data chunk

	// Mettre à jour les tailles dans l'en-tête
	header[4] = byte(chunkSize & 0xFF)
	header[5] = byte((chunkSize >> 8) & 0xFF)
	header[6] = byte((chunkSize >> 16) & 0xFF)
	header[7] = byte((chunkSize >> 24) & 0xFF)
	header[40] = byte(outSize & 0xFF)
	header[41] = byte((outSize >> 8) & 0xFF)
	header[42] = byte((outSize >> 16) & 0xFF)
	header[43] = byte((outSize >> 24) & 0xFF)

	// Write the updated header back to the file
	_, err = outFile.Seek(0, 0)
	if err != nil {
		return fmt.Errorf("erreur lors du repositionnement au début du fichier : %w", err)
	}
	_, err = outFile.Write(header)
	if err != nil {
		return fmt.Errorf("erreur lors de la réécriture de l'en-tête : %w", err)
	}

	return nil
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
	var audioformat string = ""
	var matrixformat string = ""
	var showHelp bool

	flag.StringVar(&input, "input", "", "Read audio Wave File")
	flag.StringVar(&audioformat, "audioformat", "", "is optional : value must be 4.0 or 5.1 (experimental)")
	flag.StringVar(&matrixformat, "matrixformat", "", "is optional : value must be SQ or QS ")

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

	filename := fileNameExtract(input)
	var frontLeft, frontRight, centerTime, lfeTime, backLeft, backRight []float64
	var filename5_1Channels, filename4Channels string

	switch {
	case audioformat == "5.1":
		{
			if matrixformat == "QS" {
				filename5_1Channels = filename + "_QS_5_1" + ".wav"
				log.Info("Write output 5.1 channels..(experimental)...QS decoding", "ouput", filename5_1Channels)
				frontLeft, frontRight, centerTime, lfeTime, backLeft, backRight = DecodeQSTo5_1(LT, RT)
			} else {
				filename5_1Channels = filename + "_5_1" + ".wav"
				log.Info("Write output 5.1 channels..(experimental)...SQ decoding", "ouput", filename5_1Channels)
				frontLeft, frontRight, centerTime, lfeTime, backLeft, backRight = DecodeSQTo5_1(LT, RT)
			}

			err = writeWaveFile5_1(filename5_1Channels, sampleRate, frontLeft, frontRight, centerTime, lfeTime, backLeft, backRight)
			if err != nil {
				log.Error("Failed to write output 5.1 chanels:", "error", err)
				return
			}
		}

	case audioformat != "": // 4.0
		{
			if matrixformat == "QS" {
				filename4Channels = filename + "_QS_4_0" + ".wav"
				log.Info("Write output 4.0 channels...", "ouput", filename4Channels)
				frontLeft, frontRight, backLeft, backRight = DecodeQS(LT, RT)
			} else {
				filename4Channels = filename + "_4_0" + ".wav"
				log.Info("Write output 4.0 channels...", "ouput", filename4Channels)
				frontLeft, frontRight, backLeft, backRight = DecodeSQ(LT, RT)
			}
			err = writeWaveFile4_0(filename4Channels, sampleRate, frontLeft, frontRight, backLeft, backRight)
			if err != nil {
				log.Error("Failed to write output 4.0 chanels:", "error", err)
				return
			}
		}

	default:
		{
			var filenameBackChanels, filenameFrontChanels string
			if matrixformat == "QS" {
				filenameBackChanels = "output_back_QS_" + filename + ".wav"
				filenameFrontChanels = "output_front_QS_" + filename + ".wav"
				frontLeft, frontRight, backLeft, backRight = DecodeQS(LT, RT)
				log.Info("Write output back QS channels...", "ouput", filenameBackChanels)
				err = writeWaveFile(filenameBackChanels, sampleRate, backLeft, backRight)
				if err != nil {
					log.Error("QS: Failed to write output back channels:", "error", err)
					return
				}
				log.Info("Write output front QS channels...", "ouput", filenameFrontChanels)
				err = writeWaveFile(filenameFrontChanels, sampleRate, frontLeft, frontRight)
				if err != nil {
					log.Error("Failed to write output front chanels:", "error", err)
					return
				}

			} else {
				filenameBackChanels = "output_back_" + filename + ".wav"
				filenameFrontChanels = "output_front_" + filename + ".wav"
				frontLeft, frontRight, backLeft, backRight = DecodeSQ(LT, RT)
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

		}
	}

}
