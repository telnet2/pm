package watcher

import (
	"os"
	"runtime"
	"testing"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/telnet2/pm/pubsub"
)

func asyncWriteFile(name, content string) {
	// On linux, this will send two events if the file is created
	// one for creating it, one for writing to it
	f, err := os.OpenFile(name, os.O_RDWR|os.O_APPEND|os.O_CREATE, 0644)
	Expect(err).NotTo(HaveOccurred())
	_, err = f.WriteString(content)
	Expect(err).NotTo(HaveOccurred())
	err = f.Close()
	Expect(err).NotTo(HaveOccurred())
}

var _ = Describe("test_watcher_spec", Ordered, func() {
	var (
		watcherEvent  *pubsub.ElemPublisher
		watcher       *Watcher
		eventListener chan pubsub.Elem
		observerErr   chan error
		operatingSys  string
	)
	err := os.MkdirAll("testdata/project1/src", os.ModePerm)
	Expect(err).NotTo(HaveOccurred())
	err = os.MkdirAll("testdata/project2/src", os.ModePerm)
	Expect(err).NotTo(HaveOccurred())
	operatingSys = runtime.GOOS

	BeforeAll(func() {
		watcherEvent = pubsub.NewElemPublisher(time.Second, 10)
		watcher = NewWatcher(watcherEvent)
	})

	It("should register testdata/project1 as a watcher set", func() {
		err := watcher.Add(&WatcherSet{Name: "testdata/project1", Watch: []string{"testdata/project1/**/*"}})
		Expect(err).NotTo(HaveOccurred())
		err = watcher.Add(&WatcherSet{Name: "testdata/project2", Watch: []string{"testdata/project2/**/*"}})
		Expect(err).NotTo(HaveOccurred())

		observerErr = watcher.Start()
	})

	It("should start a watcher and send an event when a file is created", func() {
		eventListener = watcherEvent.Subscribe()
		asyncWriteFile("./testdata/project1/src/file1.txt", "hello")

		select {
		case err := <-observerErr:
			if err != nil {
				Fail("Error while observing files, err=" + err.Error())
				watcher.Dispose()
			}
		}

		v := <-eventListener
		e, _ := v.(*WatchEvent)
		Expect(e.Name).To(Equal("testdata/project1"))
		Expect(e.Path).To(Equal("testdata/project1/src/file1.txt"))
		Expect(e.Type).To(Equal("CREATE"))
	})

	// This test will only run on linux
	// Mac won't send a write notification after creating and writing to the file
	// Linux does
	if operatingSys == "linux" {
		It("Should only run in linux and report a WRITE event in testdata/project1/src/file1.txt", func() {
			select {
			case err := <-observerErr:
				if err != nil {
					Fail("Error while observing files, err=" + err.Error())
					watcher.Dispose()
				}
			}

			v := <-eventListener
			e, _ := v.(*WatchEvent)
			Expect(e.Name).To(Equal("testdata/project1"))
			Expect(e.Path).To(Equal("testdata/project1/src/file1.txt"))
			Expect(e.Type).To(Equal("WRITE"))
		})
	}

	It("should send an event when the file is modified", func() {
		asyncWriteFile("./testdata/project1/src/file1.txt", "HELLO")

		select {
		case err := <-observerErr:
			if err != nil {
				Fail("Error while observing files, err=" + err.Error())
				watcher.Dispose()
			}
		}

		v := <-eventListener
		e, _ := v.(*WatchEvent)
		Expect(e.Name).To(Equal("testdata/project1"))
		Expect(e.Path).To(Equal("testdata/project1/src/file1.txt"))
		Expect(e.Type).To(Equal("WRITE"))
	})

	It("should send an event when a file is deleted", func() {
		err := os.Remove("./testdata/project1/src/file1.txt")
		Expect(err).NotTo(HaveOccurred())

		select {
		case err := <-observerErr:
			if err != nil {
				Fail("Error while observing files, err=" + err.Error())
				watcher.Dispose()
			}
		}

		v := <-eventListener
		e, _ := v.(*WatchEvent)
		Expect(e.Name).To(Equal("testdata/project1"))
		Expect(e.Path).To(Equal("testdata/project1/src/file1.txt"))
		Expect(e.Type).To(Equal("REMOVE"))
	})

	It("should send an event for project2", func() {
		asyncWriteFile("./testdata/project2/src/file1.txt", "world")

		select {
		case err := <-observerErr:
			if err != nil {
				Fail("Error while observing files, err=" + err.Error())
				watcher.Dispose()
			}
		}

		v := <-eventListener
		e, _ := v.(*WatchEvent)
		Expect(e.Name).To(Equal("testdata/project2"))
		Expect(e.Path).To(Equal("testdata/project2/src/file1.txt"))
		Expect(e.Type).To(Equal("CREATE"))
	})

	if operatingSys == "linux" {
		It("Should only run in linux and report a WRITE event in testdata/project2/src/file1.txt", func() {
			select {
			case err := <-observerErr:
				if err != nil {
					Fail("Error while observing files, err=" + err.Error())
					watcher.Dispose()
				}
			}

			v := <-eventListener
			e, _ := v.(*WatchEvent)
			Expect(e.Name).To(Equal("testdata/project2"))
			Expect(e.Path).To(Equal("testdata/project2/src/file1.txt"))
			Expect(e.Type).To(Equal("WRITE"))
		})
	}

	It("should send an event after deleting a file in project2", func() {
		err := os.Remove("./testdata/project2/src/file1.txt")
		Expect(err).NotTo(HaveOccurred())

		select {
		case err := <-observerErr:
			if err != nil {
				Fail("Error while observing files, err=" + err.Error())
				watcher.Dispose()
			}
		}

		v := <-eventListener
		e, _ := v.(*WatchEvent)
		Expect(e.Name).To(Equal("testdata/project2"))
		Expect(e.Path).To(Equal("testdata/project2/src/file1.txt"))
		Expect(e.Type).To(Equal("REMOVE"))
	})

	Context("Causing errors", func() {
		It("should report a 'start already running error'", func() {
			observerErr2 := watcher.Start()
			select {
			case err := <-observerErr2:
				Expect(err).NotTo(BeNil())
				// Don't dispose the watcher
			}
		})

		It("should report a 'we already have a start running'", func() {
			err := watcher.Add(&WatcherSet{Name: "testdata/project3", Watch: []string{"testdata/project3/**/*"}})
			Expect(err).To(HaveOccurred())
		})

		It("should report an error when adding a nil watcherset", func() {
			badWatcherEvent := pubsub.NewElemPublisher(time.Second, 10)
			badWatcher := NewWatcher(badWatcherEvent)
			err := badWatcher.Add(nil)
			Expect(err).To(HaveOccurred())
		})

		It("should report an error when adding a watcherset with no paths", func() {
			badWatcherEvent := pubsub.NewElemPublisher(time.Second, 10)
			badWatcher := NewWatcher(badWatcherEvent)
			err := badWatcher.Add(&WatcherSet{Name: "testdata/project3", Watch: []string{}})
			Expect(err).To(HaveOccurred())
		})
	})

	AfterAll(func() {
		watcher.Dispose()
	})
})

func TestWatcher(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "TestWatcher")
}
