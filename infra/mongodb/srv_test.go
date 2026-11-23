package mongodb_test

import (
	"context"
	"fmt"
	"net"
	"testing"
	"testing/synctest"
	"time"

	"github.com/fangfengxiang/kronos/infra/mongodb"
	"go.mongodb.org/mongo-driver/v2/x/mongo/driver/dns"
)

func TestOpenHonorsContextWhileResolvingSRV(t *testing.T) {
	originalResolver := dns.DefaultResolver
	t.Cleanup(func() { dns.DefaultResolver = originalResolver })
	for _, want := range []error{context.Canceled, context.DeadlineExceeded} {
		t.Run(want.Error(), func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				lookupStarted := make(chan struct{})
				releaseLookup := make(chan struct{})
				defer close(releaseLookup)
				dns.DefaultResolver = &dns.Resolver{
					LookupTXT: func(string) ([]string, error) {
						close(lookupStarted)
						<-releaseLookup
						return nil, nil
					},
					LookupSRV: func(_, _, host string) (string, []*net.SRV, error) {
						return "", nil, &net.DNSError{Err: "no SRV record in test resolver", Name: host, IsNotFound: true}
					},
				}
				var ctx context.Context
				var cancel context.CancelFunc
				if want == context.DeadlineExceeded {
					ctx, cancel = context.WithTimeout(t.Context(), time.Second)
				} else {
					ctx, cancel = context.WithCancel(t.Context())
				}
				defer cancel()
				result := make(chan error, 1)
				go func() {
					database, err := mongodb.Open(ctx, mongodb.Config{
						URI: "mongodb+srv://cluster.example.test", Database: "profiles",
					})
					if database != nil {
						_ = database.Close(ctx)
						err = fmt.Errorf("Open returned a database after cancellation: %v", err)
					}
					result <- err
				}()
				synctest.Wait()
				select {
				case <-lookupStarted:
				default:
					t.Fatal("driver did not enter the SRV URI's TXT lookup")
				}
				if want == context.Canceled {
					cancel()
				}
				<-ctx.Done()
				synctest.Wait()
				select {
				case err := <-result:
					if err != want {
						t.Fatalf("Open returned %v, want %v", err, want)
					}
				default:
					t.Fatal("Open waited for the blocked DNS lookup after the context ended")
				}
			})
		})
	}
}
