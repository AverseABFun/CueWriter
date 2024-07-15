package main

/*
#cgo LDFLAGS: -L./libcue/build -lcue
#include <fcntl.h>
#include <unistd.h>
#include <scsi/sg.h>
#include <string.h>
#include <stdlib.h>
#include <sys/ioctl.h>
#include "libcue/libcue.h"

#define SENSE_BUFF_LEN 32

int send_scsi_command(const char *device, const unsigned char *cmd, int cmd_len, unsigned char *response, int response_len, unsigned char *sense_buffer, int sense_buffer_len) {
    int sg_fd;
    sg_io_hdr_t io_hdr;

	if (sense_buffer_len < SENSE_BUFF_LEN) {
		return -3;
	}

    sg_fd = open(device, O_RDWR);
    if (sg_fd < 0) {
        return -1;
    }

    memset(&io_hdr, 0, sizeof(sg_io_hdr_t));
    io_hdr.interface_id = 'S';
    io_hdr.cmd_len = cmd_len;
    io_hdr.mx_sb_len = sense_buffer_len;
    io_hdr.dxfer_direction = SG_DXFER_FROM_DEV;
    io_hdr.dxfer_len = response_len;
    io_hdr.dxferp = response;
    io_hdr.cmdp = (unsigned char *)cmd;
    io_hdr.sbp = sense_buffer;
    io_hdr.timeout = 20000;

    if (ioctl(sg_fd, SG_IO, &io_hdr) < 0) {
        close(sg_fd);
        return -2;
    }

    if ((io_hdr.info & SG_INFO_OK_MASK) != SG_INFO_OK) {
        close(sg_fd);
        return io_hdr.info;
    }

    close(sg_fd);
    return 0;
}
*/
import "C"

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"unsafe"

	ffmpeg_go "github.com/u2takey/ffmpeg-go"
)

var command_test_unit_ready = []byte{0x00, 0x00, 0x00, 0x00, 0x00, 0x00}
var command_read_capacity = []byte{0x25, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00}
var command_get_type = []byte{0x12, 0x00, 0x00, 0x00, 0x05, 0x00}
var command_sense = []byte{0x03, 0x00, 0x00, 0x00, 96, 0x00}
var command_mode_sense = []byte{0x1A, 0x00, 0x00, 0x00, 96, 0x00}
var start_unit = []byte{0x1B, 0x00, 0x00, 0x00, 0x01, 0x00}
var stop_unit = []byte{0x1B, 0x00, 0x00, 0x00, 0x00, 0x00}

type sense_code struct {
	code int
	desc string
}

var sense_codes = []sense_code{
	{0x00000, "No sense data present"},
	{0x23A00, "Disc not present"},
	{0x23A01, "Disc not present - tray is open"},
	{0x23A02, "Disc not present"},
	{0x20401, "Becoming ready"},
	{0x20404, "Format in progress"},
	{0x20409, "Self-test in progress"},
	{0x20422, "Power cycle required"},
	{0x23100, "Disc damaged or corrupt"},
	{0x23101, "Format failed"},
	{0x43E00, "Logical unit has not self-configured yet"},
	{0x43E01, "Logical unit failure"},
	{0x52500, "Logical unit doesn't support command"},
	{0x20400, "Logical unit not ready - cause not reportable"},
	{0x23000, "Incompatible medium installed"},
}

var sync_header = []byte{0x00, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0x00}

const FRAME_AUDIO_SIZE = 2352
const FRAME_SUBCODE_SIZE = 96

const VERSION = "0.1.0"

var dummy = flag.Bool("dummy", false, "set the laser to a level where it writes nothing, but do all else normally.")
var speed = flag.Float64("speed", 4.0, "set the writing speed. 4 is the default speed.")
var get_error = flag.Int("get-error", 0, "get an error message for a specific error code")
var help = flag.Bool("help", false, "show this help message")
var ffmpeg_path = flag.String("ffmpeg-path", "", "path to the ffmpeg executable - if not set, system PATH will be searched")

func sendSCSICommand(device string, cmd []byte) ([]byte, []byte, int) {
	var response = make([]byte, 96) // Adjust size as needed
	var sense_buffer = make([]byte, C.SENSE_BUFF_LEN)

	deviceC := C.CString(device)
	defer C.free(unsafe.Pointer(deviceC))

	cmdC := C.CBytes(cmd)
	defer C.free(unsafe.Pointer(cmdC))

	ret := C.send_scsi_command(
		deviceC,
		(*C.uchar)(cmdC),
		C.int(len(cmd)),
		(*C.uchar)(unsafe.Pointer(&response[0])),
		C.int(len(response)),
		(*C.uchar)(unsafe.Pointer(&sense_buffer[0])),
		C.int(len(sense_buffer)),
	)

	if ret != 0 {
		return sense_buffer, response, int(ret)
	}

	return sense_buffer, response, 0
}

func convertToWav(input string, output string) error {
	var stream = ffmpeg_go.Input(input).
		Output(output, ffmpeg_go.KwArgs{"ar": 44100, "sample_fmt": "s16", "ac": 2}).
		OverWriteOutput().Silent(true)
	if ffmpeg_path != nil && *ffmpeg_path != "" {
		stream.SetFfmpegPath(*ffmpeg_path)
	}
	return stream.Run()
}

func generateCombinedSenseCode(sense_data []byte) int32 {
	var sense_key = sense_data[2] & 0x0F
	var asc = sense_data[12]
	var ascq = sense_data[13]
	var combined = (int32(asc) << 8) | int32(ascq)
	combined = (int32(sense_key) << 16) | combined
	return combined
}

func convertPointerToString(ptr *C.char) string {
	var output = ""
	for *ptr != 0 {
		output += string(*ptr)
		ptr = (*C.char)(unsafe.Pointer(uintptr(unsafe.Pointer(ptr)) + 1))
	}
	return output
}

func main() {
	fmt.Println("cuewriter v" + VERSION + ` - a tool to write CUE files to CDs
	AGPL license, written by Averse
	(c) 2024 Averse`)
	fmt.Println()

	flag.Usage = func() {
		fmt.Fprintf(flag.CommandLine.Output(), "Usage: %s [options] <cuefile> <device>\n", os.Args[0])
		fmt.Fprintf(flag.CommandLine.Output(), "Options:\n")
		flag.PrintDefaults()
	}

	flag.Parse()

	if *get_error != 0 {
		fmt.Print("Error code ", *get_error, " means: ")
		var actual_error = int32(*get_error)
		if actual_error > 0 {
			actual_error = int32(-(^(int8(actual_error)) + 1))
		}
		switch actual_error {
		case -1:
			fmt.Println("Error opening device. This could mean that the device does not exist, or that you do not have permission to access it.")
		case -2:
			fmt.Println("Error sending SCSI command. This could mean that the device does not support the command, or that the command is invalid.")
		case -3:
			fmt.Println("Error getting SCSI response. This could mean that the device did not respond, or that the response is invalid.")
		default:
			fmt.Println("Unknown error code.")
		}
		return
	}

	if flag.NArg() == 0 || flag.Arg(0) == "" || *help {
		flag.Usage()
		return
	}

	var _, inquiry_data5, inquiry_error5 = sendSCSICommand(flag.Arg(0), command_get_type)
	if inquiry_error5 != 0 {
		fmt.Println("Error getting device information: SCSI error", inquiry_error5)
		os.Exit(inquiry_error5)
	}
	if inquiry_data5[0]&(0x05) != 0x05 {
		fmt.Println("Device is not a CD/DVD drive.")
		return
	}

	var ready_sense, _, _ = sendSCSICommand(flag.Arg(0), command_test_unit_ready)
	var combined = generateCombinedSenseCode(ready_sense)
	for sense_codes_index := range sense_codes {
		if combined == 0 {
			break
		}
		if sense_codes[sense_codes_index].code == int(combined) {
			fmt.Println("Logical unit is not ready:", sense_codes[sense_codes_index].desc)
			switch sense_codes[sense_codes_index].code {
			case 0x20401:
				fmt.Println("Waiting for the drive to become ready...")
				sendSCSICommand(flag.Arg(0), start_unit)
				for {
					ready_sense, _, _ = sendSCSICommand(flag.Arg(0), command_test_unit_ready)
					combined = generateCombinedSenseCode(ready_sense)
					if combined != 0x20401 {
						break
					}
				}
				fmt.Println("Drive is ready.")
			case 0:
				return
			default:
				return
			}
		}
	}
	var file, fileerror = os.OpenFile(flag.Arg(1), os.O_RDONLY, 0)
	if fileerror != nil {
		fmt.Println("Error opening file:", fileerror)
		return
	}
	fileInfo, err := file.Stat()
	if err != nil {
		fmt.Println("Error getting file information:", err)
		return
	}
	var filedata = make([]byte, fileInfo.Size())
	file.Read(filedata)
	var cueCD = C.cue_parse_string((*C.char)(unsafe.Pointer(&(filedata[0]))))
	file.Close()
	var numTracks = int32(C.cd_get_ntrack(cueCD))
	if numTracks == 0 {
		fmt.Println("No tracks found in CUE file.")
		return
	}
	if numTracks > 99 {
		fmt.Println("Warning: More than 99 tracks found in CUE file. This may not be supported by all drives.")
	}
	var diskText = C.cd_get_cdtext(cueCD)
	fmt.Println("Writing", convertPointerToString(C.cdtext_get(C.PTI_TITLE, diskText)), "by", convertPointerToString(C.cdtext_get(C.PTI_PERFORMER, diskText)), "to", flag.Arg(1))

	var tempDir, error = os.MkdirTemp("", "cuewriter")
	if error != nil {
		fmt.Println("Error creating temporary directory:", error)
		return
	}
	os.Chmod(tempDir, os.ModeTemporary|755)

	for i := int32(1); i <= numTracks; i++ {
		var track = C.cd_get_track(cueCD, C.int(i))
		var trackFile = convertPointerToString(C.track_get_filename(track))
		if !(strings.HasPrefix(trackFile, "/")) {
			trackFile = filepath.Dir(flag.Arg(1)) + "/" + trackFile
		}
		trackFile = strings.ReplaceAll(trackFile, "\\", "/")
		//var trackText = C.track_get_cdtext(track)
		fmt.Printf("Writing track %02d\n", int(i))
		var err3 = convertToWav(trackFile, tempDir+"/track"+fmt.Sprintf("%02d", i)+".wav")
		if err3 != nil {
			fmt.Println("Error converting track to WAV:", err3)
			return
		}
		file, err := os.Open(tempDir + "/track" + fmt.Sprintf("%02d", i) + ".wav")
		if err != nil {
			fmt.Println("Error opening temporary file:", err)
			return
		}
		info, err2 := file.Stat()
		if err2 != nil {
			fmt.Println("Error getting temporary file information:", err2)
			return
		}
		wav_data := make([]byte, info.Size()-44)
		if _, err := file.ReadAt(wav_data, 44); err != nil {
			fmt.Println("Error reading temporary file:", err)
			return
		}
		defer file.Close()
		divided_data_l := make([][]byte, len(wav_data)/6)
		divided_data_r := make([][]byte, len(wav_data)/6)
		for j := 0; j < len(wav_data)/12; j += 1 {
			for g := 0; g < 12; g += 1 {
				if g%2 == 0 {
					divided_data_l[j] = append(divided_data_l[j], wav_data[j*12+g])
				} else {
					divided_data_r[j] = append(divided_data_r[j], wav_data[j*12+g])
				}
			}
		}

		frames := make([][]byte, len(divided_data_l)/74)
		for frame := range frames {
			// REMEMBER THAT LAST BYTE SHOULD ONLY BE THE FIRST 4 BITS
			frames[frame] = make([]byte, 74)
			for i := 0; i < 74; i += 1 {
				frames[frame][i] = 0
			}
		}
	}
}
