package bioris

import (
	"context"
	"fmt"
	"io"
	"time"

	"github.com/alice-lg/alice-lg/backend/api"
	pb "github.com/bio-routing/bio-rd/cmd/multiris/api"
	bnet "github.com/bio-routing/bio-rd/net"
	bnetapi "github.com/bio-routing/bio-rd/net/api"
	brouteapi "github.com/bio-routing/bio-rd/route/api"
	"github.com/pkg/errors"
	"google.golang.org/grpc"
	"google.golang.org/grpc/connectivity"
)

const (
	apiVersion = "v0.1.0"
)

// BioRIS represents a connection towards a Bio RIS
type BioRIS struct {
	config Config

	grpcConn *grpc.ClientConn
}

// NewBioRIS creates a new BioRIS
func NewBioRIS(config Config) *BioRIS {
	fmt.Printf("Initializing BioRIS connector\n")
	fmt.Printf("Config is %v\n", config)
	fmt.Printf("BioRIS host is %v, router is %v\n", config.API, config.Router)

	return &BioRIS{
		config: config,
	}
}

// ExpireCaches expires all caches, but currently we do not have any
func (br *BioRIS) ExpireCaches() int {
	return 0
}

// Status returns the current status of BioRIS
func (br *BioRIS) Status() (*api.StatusResponse, error) {
	resp := &api.StatusResponse{
		Api: getDefaultApiStatus(),
		Status: api.Status{
			// FIXME: currently not from API
			ServerTime: time.Now(),
			Version:    apiVersion,
			Backend:    "BioRIS",
		},
	}

	return resp, nil
}

// Neighbours returns all neighbours this router has
func (br *BioRIS) Neighbours() (*api.NeighboursResponse, error) {
	risclient, err := br.getRISClient()
	if err != nil {
		return nil, errors.Wrap(err, "Could not get RIS client")
	}

	client, err := risclient.GetNeighbors(context.Background(), &pb.GetNeighborsRequest{
		Router: br.config.Router,
		//VrfId:   br.config.VRFID,
	})
	if err != nil {
		return nil, errors.Wrap(err, "Could not get neighbors")
	}

	neighbours := make([]*api.Neighbour, 0)
	for _, bioNeighbor := range client.Neighbors {
		state := bioNeighbor.Status.String()
		if state == "Established" {
			// alice only expects "up" as established/up state
			state = "up"
		}

		uptime := time.Duration(0)
		if bioNeighbor.EstablishedSince > 0 {
			// uptime = time.Since(time.Unix(int64(bioNeighbor.EstablishedSince), 0))
			uptime = time.Since(time.Unix(int64(bioNeighbor.EstablishedSince), 0))
		}
		neighbour := &api.Neighbour{
			Id: bnet.IPFromProtoIP(bioNeighbor.NeighborAddress).String(),

			Address:         bnet.IPFromProtoIP(bioNeighbor.NeighborAddress).String(),
			Asn:             int(bioNeighbor.PeerAsn),
			State:           state,
			Description:     bioNeighbor.Description,
			RoutesReceived:  int(bioNeighbor.Stats.RoutesReceived),
			RoutesFiltered:  0,
			RoutesExported:  int(bioNeighbor.Stats.RoutesExported),
			RoutesPreferred: 0,
			RoutesAccepted:  0,
			Uptime:          uptime,
			LastError:       "",
			RouteServerId:   br.config.Id,
		}
		neighbours = append(neighbours, neighbour)
	}

	resp := &api.NeighboursResponse{
		Api:        getDefaultApiStatus(),
		Neighbours: neighbours,
	}

	return resp, nil
}

// NeighboursStatus returns the status for each neighbour we have
func (br *BioRIS) NeighboursStatus() (*api.NeighboursStatusResponse, error) {
	risclient, err := br.getRISClient()
	if err != nil {
		return nil, errors.Wrap(err, "Could not get RIS client")
	}

	client, err := risclient.GetNeighbors(context.Background(), &pb.GetNeighborsRequest{
		Router: br.config.Router,
		//VrfId:   br.config.VRFID,
		//Afisafi: 0,
	})
	if err != nil {
		return nil, errors.Wrap(err, "Could not get neighbors")
	}

	neighbours := make([]*api.NeighbourStatus, 0)
	// for _, bioNeighbor := range client.Neighbors {
	for range client.Neighbors {
		neighbour := &api.NeighbourStatus{
			State: "unclear if used",
			Since: 0,
		}
		neighbours = append(neighbours, neighbour)
	}
	resp := &api.NeighboursStatusResponse{
		Api:        getDefaultApiStatus(),
		Neighbours: neighbours,
	}

	return resp, nil
}

// Routes returns all routes
func (br *BioRIS) Routes(neighbourId string) (*api.RoutesResponse, error) {
	return br.getRoutes(neighbourId)
}

// RoutesReceived returns all routes received
func (br *BioRIS) RoutesReceived(neighbourId string) (*api.RoutesResponse, error) {
	return br.getRoutes(neighbourId)
}

// RoutesFiltered returns all filtered routes, currently not implemented --> returns empty list
func (br *BioRIS) RoutesFiltered(neighbourId string) (*api.RoutesResponse, error) {
	resp := &api.RoutesResponse{
		Api:         getDefaultApiStatus(),
		Imported:    nil,
		Filtered:    make([]*api.Route, 0),
		NotExported: nil,
	}
	return resp, nil
}

// RoutesNotExported returns all not exported routes, currently not implemented --> returns empty list
func (br *BioRIS) RoutesNotExported(neighbourId string) (*api.RoutesResponse, error) {
	resp := &api.RoutesResponse{
		Api:         getDefaultApiStatus(),
		Imported:    nil,
		Filtered:    nil,
		NotExported: make([]*api.Route, 0),
	}
	return resp, nil
}

// AllRoutes returns all routes found on this router
func (br *BioRIS) AllRoutes() (*api.RoutesResponse, error) {
	return br.getRoutes("")
}

func (br *BioRIS) getRoutes(neighbor string) (*api.RoutesResponse, error) {
	risclient, err := br.getRISClient()
	if err != nil {
		return nil, errors.Wrap(err, "Could not get RIB client")
	}

	routes := make(api.Routes, 0)
	for _, afi := range []pb.DumpRIBRequest_AFISAFI{pb.DumpRIBRequest_IPv4Unicast, pb.DumpRIBRequest_IPv6Unicast} {
		client, err := risclient.DumpRIB(context.Background(), &pb.DumpRIBRequest{
			Router:   br.config.Router,
			VrfId:    br.config.VRFID,
			Afisafi:  afi,
			Neighbor: neighbor,
		})
		if err != nil {
			return nil, errors.Wrap(err, "Could not dump RIB")
		}

		for {
			r, err := client.Recv()
			if err != nil {
				if err == io.EOF {
					break
				}
				return nil, errors.Wrap(err, "Receive failed")
			}
			for _, path := range r.Route.Paths {
				if path.Type == brouteapi.Path_BGP {
					routes = append(routes, makeAliceRoute(r.Route.Pfx, path.BgpPath))
				}
			}
		}
	}

	resp := &api.RoutesResponse{
		Api: api.ApiStatus{
			Version:         apiVersion,
			CacheStatus:     api.CacheStatus{},
			ResultFromCache: false,
			Ttl:             time.Now(),
		},
		Imported: routes,
	}
	return resp, nil
}

//func (br *BioRIS) getRoutes(neighbor string) (*api.RoutesResponse, error) {
//	risclient, err := br.getRISClient()
//	if err != nil {
//		return nil, errors.Wrap(err, "Could not get RIB client")
//	}
//
//    client, err := risclient.DumpRIB(context.Background(), &pb.DumpRIBRequest{
//        Router:  br.config.Router,
//        VrfId:   br.config.VRFID,
//        Afisafi: 0,
//		Neighbor: neighbor,
//    })
//	if err != nil {
//		return nil, errors.Wrap(err, "Could not dump RIB")
//	}
//
//	routes := make(api.Routes, 0)
//	for {
//		r, err := client.Recv()
//		if err != nil {
//			if err == io.EOF {
//				break
//			}
//			return nil, errors.Wrap(err, "Receive failed")
//		}
//		for _, path := range r.Route.Paths {
//			if path.Type == brouteapi.Path_BGP {
//				routes = append(routes, makeAliceRoute(r.Route.Pfx, path.BgpPath))
//			}
//		}
//	}
//
//	resp := &api.RoutesResponse{
//		Api: api.ApiStatus{
//			Version: apiVersion,
//			CacheStatus: api.CacheStatus{
//
//			},
//			ResultFromCache: false,
//			Ttl: time.Now(),
//		},
//		Imported: routes,
//	}
//	return resp, nil
//}

func (br *BioRIS) getRISClient() (pb.RoutingInformationServiceClient, error) {
	if br.grpcConn != nil {
		if br.grpcConn.GetState() != connectivity.Ready {
			br.grpcConn.Close()
			br.grpcConn = nil
		}
	}

	if br.grpcConn == nil {
		// FIXME: This should not be WithInsecure()
		conn, err := grpc.Dial(br.config.API, grpc.WithInsecure())
		if err != nil {
			return nil, errors.Wrap(err, "Could not connect to api")
		}
		br.grpcConn = conn
	}

	risclient := pb.NewRoutingInformationServiceClient(br.grpcConn)
	return risclient, nil
}

func getDefaultApiStatus() api.ApiStatus {
	return api.ApiStatus{
		Version:         apiVersion,
		CacheStatus:     api.CacheStatus{},
		ResultFromCache: false,
		Ttl:             time.Now().Add(60 * time.Second),
	}
}

func makeAliceRoute(pfx *bnetapi.Prefix, bgpPath *brouteapi.BGPPath) *api.Route {
	pfxStr := bnet.NewPrefixFromProtoPrefix(pfx).String()

	// FIXME: we need to handle as sets in path
	aspath := make([]int, 0, len(bgpPath.AsPath[0].Asns))
	for _, asn := range bgpPath.AsPath[0].Asns {
		aspath = append(aspath, int(asn))
	}

	return &api.Route{
		Id:      pfxStr,
		Network: pfxStr,
		Bgp: api.BgpInfo{
			AsPath:  aspath,
			NextHop: bnet.IPFromProtoIP(bgpPath.NextHop).String(),
			// FIXME: get communities from API
			//Communities: bgpPath.Communities,
			//LargeCommunities: bgpPath.LargeCommunities,
			Med:       int(bgpPath.Med),
			LocalPref: int(bgpPath.LocalPref),
		},
	}
}
