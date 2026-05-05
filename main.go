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
	durations := []string{"0.5", "0.8", "1.0", "1.1", "1.2", "1.3", "1.4", "1.5", "1.8", "2.0", "2.5", "3.0", "4.0", "5.0"}
	thresholds := []string{"0.05%", "0.1%", "0.15%", "0.2%", "0.25%", "0.3%", "0.4%", "0.5%", "0.75%", "1%", "1.5%", "2%", "5%", "10%", "15%"}

	if expectedTracks <= 0 {
		// Fallback to default
		durations = []string{"0.5"}
		thresholds = []string{"1%"}
		expectedTracks = -1 // Ignore track count
	}

	for _, duration := range durations {
		for _, threshold := range thresholds {
			log.Printf("Trying sox parameters: duration=%v, threshold=%v", duration, threshold)
			
			tmpDir, err := os.MkdirTemp(procDir, "sox_processing")
			if err != nil {
				return nil, err
			}
			
			outPattern := filepath.Join(tmpDir, strippedFile+"_track_.wav")
			soxCmd := exec.Command("sox", inputFile, outPattern, "silence", "1", duration, threshold, "1", duration, threshold, ":", "newfile", ":", "restart")
			output, err := soxCmd.CombinedOutput()
			
			if err != nil {
				log.Printf("Sox failed for %v/%v: %v -> %v", duration, threshold, err, string(output))
				os.RemoveAll(tmpDir)
				continue
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

			if expectedTracks == -1 || len(validTracks) == expectedTracks {
				log.Printf("Found working sox parameters: duration=%v, threshold=%v for %v tracks", duration, threshold, len(validTracks))
				
				// Move valid tracks to procDir
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

			os.RemoveAll(tmpDir)
		}
	}

	return nil, fmt.Errorf("could not find sox parameters to get %v tracks", expectedTracks)
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
		
		id, err := strconv.ParseInt(selems[0], 10, 32)
		log.Printf("Parsed to: %v, %v", id, err)
		if err != nil {
			return err
		}

		ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
		conn, err := utils.LFDialServer(ctx, "recordcollection")
		var expectedTracks int
		var rcclient pbrc.RecordCollectionServiceClient
		if err == nil {
			rcclient = pbrc.NewRecordCollectionServiceClient(conn)
			res, err := rcclient.GetRecord(ctx, &pbrc.GetRecordRequest{ReleaseId: int32(id)})
			if err == nil {
				expectedTracks = len(res.GetRecord().GetRelease().GetTracklist())
				log.Printf("Found expected tracks: %v", expectedTracks)
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
		flacFiles, err := filepath.Glob(fmt.Sprintf("%v/%v*track*.flac", dir, selems[0]))
		if err != nil {
			cancel()
			return err
		}
		args = append([]string{}, flacFiles...)
		args = append(args, fmt.Sprintf("%v/%v/", *saveDir, selems[0]))
		moveCmd := exec.Command("mv", args...)
		log.Printf("Running move command: %v", moveCmd.String())
		output, err = moveCmd.CombinedOutput()
		log.Printf("Move output: %v -> %v", err, string(output))
		// We expect errors here if the file is blank - ignore this

		// Move the original file to retained directory
		err = os.Rename(filepath.Join(dir, file.Name()), filepath.Join(retainedDir, file.Name()))
		if err != nil {
			log.Printf("Error moving file to retained: %v", err)
		}

		log.Printf("GLOB %v/%v*.wav", dir, strippedFile)
		filesToRM, err := filepath.Glob(fmt.Sprintf("%v/%v*.wav", dir, strippedFile))
		if err != nil {
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
					ReleaseId: int32(id),
				},
			})
			log.Printf("Query: %v -> %v", records, err)
			if err == nil {
				for _, record := range records.GetInstanceIds() {
					rcclient.UpdateRecord(ctx, &pbrc.UpdateRecordRequest{Reason: "digital rip", Update: &pbrc.Record{Release: &pbgd.Release{InstanceId: record}, Metadata: &pbrc.ReleaseMetadata{LastRipDate: time.Now().Unix()}}})
				}
			}
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
