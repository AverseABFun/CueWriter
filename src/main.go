package main

/* #include <fcntl.h>
#include <unistd.h>
#include <scsi/sg.h>
#include <string.h>
#include <stdlib.h>
#include <sys/ioctl.h>

#define SENSE_BUFF_LEN 32

int send_scsi_command(const char *device, const unsigned char *cmd, int cmd_len, unsigned char *response, int response_len) {
    int sg_fd;
    unsigned char sense_buffer[SENSE_BUFF_LEN];
    sg_io_hdr_t io_hdr;

    sg_fd = open(device, O_RDWR);
    if (sg_fd < 0) {
        return -1;
    }

    memset(&io_hdr, 0, sizeof(sg_io_hdr_t));
    io_hdr.interface_id = 'S';
    io_hdr.cmd_len = cmd_len;
    io_hdr.mx_sb_len = sizeof(sense_buffer);
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
        return -3;
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
	"unsafe"
)

const VERSION = "0.1.0"

var dummy = flag.Bool("dummy", false, "set the laser to a level where it writes nothing, but do all else normally.")
var speed = flag.Float64("speed", 4.0, "set the writing speed. 4 is the default speed.")
var get_error = flag.Int("get-error", 0, "get an error message for a specific error code")
var help = flag.Bool("help", false, "show this help message")

func sendSCSICommand(device string, cmd []byte) ([]byte, int) {
	var response = make([]byte, 96) // Adjust size as needed

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
	)

	if ret != 0 {
		return nil, int(ret)
	}

	return response, 0
}

func main() {
	fmt.Println("cuewriter v" + VERSION + ` - a tool to write CUE files to CDs
	AGPL license, written by Averse
	(c) 2024 Averse`)

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
	if *dummy {
		fmt.Println("Dummy mode is enabled. No data will be written to the disk.")
	}
	var inquiryData5, inquiryError5 = sendSCSICommand(flag.Arg(1), []byte{0x12, 0x00, 0x00, 0x00, 0x05, 0x00})
	if inquiryError5 != 0 {
		fmt.Println("Error getting device information: SCSI error", inquiryError5)
		os.Exit(inquiryError5)
	}
	if inquiryData5[0]&(0x05) != 0x05 {
		fmt.Println("Device is not a CD/DVD drive.")
		return
	}
}
