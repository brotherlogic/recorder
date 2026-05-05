package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/brotherlogic/goserver/utils"
	"google.golang.org/grpc"

	pbgd "github.com/brotherlogic/godiscogs/proto"
	pbrc "github.com/brotherlogic/recordcollection/proto"
	pb "github.com/brotherlogic/recorder/proto"
	pbrg "github.com/brotherlogic/recordgetter/proto"
)

var (
	port    = flag.Int("port", 8080, "Port to server from")
	procDir = flag.String("processing_dir", "/home/simon/processing/", "Directory to processing recordings")
	saveDir = flag.String("save_dir", "/home/simon/music/flacs/", "Directory to save recordings")
)

type Recorder struct {
	cmd   *exec.Cmd
	pLock sync.Mutex
}

type Server struct {
	r *Recorder
}

func getCurrentRecord() (int32, int32, error) {
	ctx, cancel := utils.ManualContext("recorder-get", time.Minute)
	defer cancel()

	conn, err := utils.LFDialServer(ctx, "recordgetter")
	if err != nil {
		log.Fatalf("Can't dial getter: %v", err)
	}
	defer conn.Close()
	client := pbrg.NewRecordGetterClient(conn)

	curr, err := client.GetRecord(ctx, &pbrg.GetRecordRequest{
		Type: pbrg.RequestType_DEFAULT,
	})

	if err != nil {
		return -1, -1, err
	}

	disk := curr.GetDisk()
	if curr.GetRecord().GetRelease().GetFormatQuantity() == 1 {
		disk = 0
	}

	return curr.GetRecord().GetRelease().GetId(), disk, nil
}

func (s *Server) cleanupRetainedFiles(dir string) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		log.Printf("Error reading retained dir: %v", err)
		return
	}

	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		info, err := entry.Info()
		if err != nil {
			log.Printf("Error getting info for %v: %v", entry.Name(), err)
			continue
		}

		if time.Since(info.ModTime()) > time.Hour*24 {
			log.Printf("Deleting retained file: %v", entry.Name())
			err := os.Remove(filepath.Join(dir, entry.Name()))
			if err != nil {
				log.Printf("Error deleting retained file: %v", err)
			}
		}
	}
}

func (s *Server) splitWithSox(inputFile string, procDir string, strippedFile string, expectedTracks int) ([]string, error) {
	duration, threshold, err := FindBestSoxParams(inputFile, expectedTracks)
	if err != nil {
		return nil, err
	}

	log.Printf("Using sox parameters: duration=%v, threshold=%v for expected %v tracks", duration, threshold, expectedTracks)

	tmpDir, err := os.MkdirTemp(procDir, "sox_processing")
	if err != nil {
		return nil, err
	}

	outPattern := filepath.Join(tmpDir, strippedFile+"_track_.wav")
	soxCmd := exec.Command("sox", inputFile, outPattern, "silence", "1", duration, threshold, "1", duration, threshold, ":", "newfile", ":", "restart")
	output, err := soxCmd.CombinedOutput()

	if err != nil {
		log.Printf("Sox failed: %v -> %v", err, string(output))
		os.RemoveAll(tmpDir)
		return nil, err
	}

	matches, err := filepath.Glob(filepath.Join(tmpDir, strippedFile+"_track_*.wav"))
	if err != nil {
		os.RemoveAll(tmpDir)
		return nil, err
	}

	var validTracks []string
	for _, match := range matches {
		info, err := os.Stat(match)
		if err == nil && info.Size() > 10000 {
			validTracks = append(validTracks, match)
		} else if err == nil {
			os.Remove(match) // Remove tiny files
		}
	}

	var finalFiles []string
	for _, track := range validTracks {
		finalFile := filepath.Join(procDir, filepath.Base(track))
		err := os.Rename(track, finalFile)
		if err != nil {
			log.Printf("Error renaming track %v: %v", track, err)
		}
		finalFiles = append(finalFiles, finalFile)
	}
	os.RemoveAll(tmpDir)
	return finalFiles, nil
}

func getDiskFromPosition(pos string) int {
	if len(pos) == 0 {
		return 0
	}

	// Find the letter part
	letters := ""
	for i := 0; i < len(pos); i++ {
		if pos[i] >= 'A' && pos[i] <= 'Z' {
			letters += string(pos[i])
		} else {
			break
		}
	}

	if len(letters) == 0 {
		return 0
	}

	// Convert letters to number (A=1, B=2, AA=27, etc)
	val := 0
	for i := 0; i < len(letters); i++ {
		val = val*26 + int(letters[i]-'A'+1)
	}

	return (val-1)/2 + 1
}

func getTrackOffset(release *pbgd.Release, disk int32) int {
	if disk <= 1 {
		return 0
	}

	offset := 0
	for _, track := range release.GetTracklist() {
		pos := track.GetPosition()
		if strings.Contains(pos, "-") {
			d, _ := strconv.Atoi(strings.Split(pos, "-")[0])
			if int32(d) < disk {
				offset++
			}
		} else if len(pos) > 0 {
			d := getDiskFromPosition(pos)
			if int32(d) < disk {
				offset++
			}
		}
	}
	return offset
}

func getExpectedTracks(release *pbgd.Release, disk int32) int {
	if release.GetFormatQuantity() <= 1 {
		return len(release.GetTracklist())
	}

	count := 0
	for _, track := range release.GetTracklist() {
		pos := track.GetPosition()
		if strings.Contains(pos, "-") {
			d, _ := strconv.Atoi(strings.Split(pos, "-")[0])
			if int32(d) == disk {
				count++
			}
		} else if len(pos) > 0 {
			d := getDiskFromPosition(pos)
			if int32(d) == disk {
				count++
			}
		}
	}
	return count
}

func (s *Server) processFiles(dir string) error {
	// Lock to prevent stomping
	s.r.pLock.Lock()
	defer s.r.pLock.Unlock()

	retainedDir := filepath.Join(dir, "retained")
	err := os.MkdirAll(retainedDir, 0755)
	if err != nil {
		log.Printf("Error creating retained dir: %v", err)
	}
	s.cleanupRetainedFiles(retainedDir)

	entries, err := os.ReadDir(dir)
	if err != nil {
		return err
	}
	for _, file := range entries {
		log.Printf("Processing file: %v", file.Name())

		if file.IsDir() || !strings.HasSuffix(file.Name(), ".wav") {
			log.Printf("INVALID FILE: %v", file.Name())
			continue
		}

		strippedFile := file.Name()[:len(file.Name())-4]
		elems := strings.Split(file.Name(), "-")
		selems := strings.Split(elems[0], "_")

		id64, err := strconv.ParseInt(selems[0], 10, 32)
		log.Printf("Parsed to: %v, %v", id64, err)
		if err != nil {
			return err
		}
		id := int32(id64)

		disk := int32(0)
		if len(selems) > 1 {
			d, _ := strconv.ParseInt(selems[1], 10, 32)
			disk = int32(d)
		}

		ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
		conn, err := utils.LFDialServer(ctx, "recordcollection")
		var expectedTracks int
		var offset int
		var rcclient pbrc.RecordCollectionServiceClient
		if err == nil {
			rcclient = pbrc.NewRecordCollectionServiceClient(conn)
			res, err := rcclient.GetRecord(ctx, &pbrc.GetRecordRequest{ReleaseId: id})
			if err == nil {
				expectedTracks = getExpectedTracks(res.GetRecord().GetRelease(), disk)
				log.Printf("Found expected tracks: %v", expectedTracks)
				offset = getTrackOffset(res.GetRecord().GetRelease(), disk)
			} else {
				log.Printf("Error getting record for expected tracks: %v", err)
			}
		} else {
			log.Printf("Dialled RC for expected tracks error: %v", err)
		}

		// Process the file
		inputFile := filepath.Join(dir, file.Name())
		files, err := s.splitWithSox(inputFile, dir, strippedFile, expectedTracks)
		if err != nil {
			log.Printf("Error splitting with sox: %v", err)
			if conn != nil {
				conn.Close()
			}
			cancel()
			return err
		}

		// Rename the tracks with the offset
		for i := len(files); i > 0; i-- {
			oldName := filepath.Join(dir, fmt.Sprintf("%v_track_%03d.wav", strippedFile, i))
			if _, err := os.Stat(oldName); os.IsNotExist(err) {
				oldName = filepath.Join(dir, fmt.Sprintf("%v_track_%d.wav", strippedFile, i))
			}
			newName := filepath.Join(dir, fmt.Sprintf("%v_track_%03d.wav", strippedFile, i+offset))
			os.Rename(oldName, newName)
		}

		// Re-glob to get updated names
		files, err = filepath.Glob(filepath.Join(dir, fmt.Sprintf("%v_track_*.wav", strippedFile)))
		if err != nil {
			if conn != nil {
				conn.Close()
			}
			cancel()
			return err
		}

		// Convert to flac
		args := []string{"--best", "--delete-input-file", "--output-prefix", dir + "/"}
		args = append(args, files...)
		flacCmd := exec.Command("flac", args...)
		log.Printf("Running flac command: %v", flacCmd.String())
		output, _ := flacCmd.CombinedOutput()
		log.Printf("Flac output: %v -> %v", err, string(output))

		//Move file into save dir - we don't care if this fails
		err = os.Mkdir(fmt.Sprintf("%v/%v", *saveDir, selems[0]), 0755)
		log.Printf("Error in mkdir: %v", err)
		flacFiles, err := filepath.Glob(filepath.Join(dir, fmt.Sprintf("%v*track*.flac", selems[0])))
		if err != nil {
			if conn != nil {
				conn.Close()
			}
			cancel()
			return err
		}
		args = append([]string{}, flacFiles...)
		args = append(args, fmt.Sprintf("%v/%v/", *saveDir, selems[0]))
		moveCmd := exec.Command("mv", args...)
		log.Printf("Running move command: %v", moveCmd.String())
		output, err = moveCmd.CombinedOutput()
		log.Printf("Move output: %v -> %v", err, string(output))

		// Move the original file to retained directory
		err = os.Rename(filepath.Join(dir, file.Name()), filepath.Join(retainedDir, file.Name()))
		if err != nil {
			log.Printf("Error moving file to retained: %v", err)
		}

		log.Printf("GLOB %v/%v*.wav", dir, strippedFile)
		filesToRM, err := filepath.Glob(filepath.Join(dir, fmt.Sprintf("%v*.wav", strippedFile)))
		if err != nil {
			if conn != nil {
				conn.Close()
			}
			cancel()
			return err
		}
		if len(filesToRM) > 0 {
			rmCmd := exec.Command("rm", append([]string{}, filesToRM...)...)
			out, err := rmCmd.CombinedOutput()
			log.Printf("RM %v -> %v", err, string(out))
		}

		if rcclient != nil {
			records, err := rcclient.QueryRecords(ctx, &pbrc.QueryRecordsRequest{
				Query: &pbrc.QueryRecordsRequest_ReleaseId{
					ReleaseId: id,
				},
			})
			log.Printf("Query: %v -> %v", records, err)
			if err == nil {
				for _, record := range records.GetInstanceIds() {
					rcclient.UpdateRecord(ctx, &pbrc.UpdateRecordRequest{Reason: "digital rip", Update: &pbrc.Record{Release: &pbgd.Release{InstanceId: record}, Metadata: &pbrc.ReleaseMetadata{LastRipDate: time.Now().Unix()}}})
				}
			}
			conn.Close()
		}
		cancel()
	}

	return nil
}

func (r *Recorder) runRecord() error {
	log.Printf("Running record")
	num, disk, err := getCurrentRecord()
	if err != nil {
		return err
	}

	// Start recording
	date := time.Now().Format("2006-01-02")
	diskRef := fmt.Sprintf("%v_%v-%v.wav", num, disk, date)
	if disk == 0 {
		diskRef = fmt.Sprintf("%v-%v.wav", num, date)
	}

	r.cmd = exec.Command("arecord", "--device", "hw:0,0", "--format", "S32_LE", "--rate", "44100", "--channels", "4", diskRef)
	log.Printf("Starging record")
	output, err := r.cmd.CombinedOutput()
	log.Printf("Error: %v -> %v", err, string(output))
	r.cmd.Wait()

	files, err := filepath.Glob(fmt.Sprintf("%v*.wav", num))
	if err != nil {
		return err
	}
	args := append([]string{}, files...)
	args = append(args, *procDir)

	// Move all the files over to the processing directory
	moveCmd := exec.Command("mv", args...)
	log.Printf("Copying files")
	output, err = moveCmd.CombinedOutput()
	log.Printf("Moved files %v -> %v", err, string(output))

	if err != nil {
		return err
	}

	return nil
}

func (s *Server) NewRecord(ctx context.Context, _ *pb.NewRecordRequest) (*pb.NewRecordResponse, error) {
	// Halt the current recording
	c := exec.Command("killall", "arecord")
	c.Start()
	c.Wait()
	return &pb.NewRecordResponse{}, nil
}

func main() {
	r := &Recorder{}
	s := &Server{r: r}

	go func() {
		for {
			err := r.runRecord()
			log.Printf("Error recording: %v", err)
			time.Sleep(time.Second * 5)
			go func() { s.processFiles(*procDir) }()
		}
	}()

	lis, err := net.Listen("tcp", fmt.Sprintf(":%d", *port))
	if err != nil {
		log.Fatalf("failed to listen: %v", err)
	}
	var opts []grpc.ServerOption
	grpcServer := grpc.NewServer(opts...)
	pb.RegisterRecordGetterServer(grpcServer, s)
	err = grpcServer.Serve(lis)
	log.Printf("Error serving: %v", err)
}
