//                           _       _
// __      _____  __ ___   ___  __ _| |_ ___
// \ \ /\ / / _ \/ _` \ \ / / |/ _` | __/ _ \
//  \ V  V /  __/ (_| |\ V /| | (_| | ||  __/
//   \_/\_/ \___|\__,_| \_/ |_|\__,_|\__\___|
//
//  Copyright © 2016 - 2019 Weaviate. All rights reserved.
//  LICENSE WEAVIATE OPEN SOURCE: https://www.semi.technology/playbook/playbook/contract-weaviate-OSS.html
//  LICENSE WEAVIATE ENTERPRISE: https://www.semi.technology/playbook/contract-weaviate-enterprise.html
//  CONCEPT: Bob van Luijt (@bobvanluijt)
//  CONTACT: hello@semi.technology
//

// +build integrationTest

package esvector

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/elastic/go-elasticsearch/v5"
	"github.com/semi-technologies/weaviate/entities/models"
	"github.com/semi-technologies/weaviate/entities/schema"
	"github.com/semi-technologies/weaviate/entities/schema/kind"
	"github.com/semi-technologies/weaviate/entities/search"
	"github.com/sirupsen/logrus/hooks/test"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestEsVectorCache(t *testing.T) {
	client, err := elasticsearch.NewClient(elasticsearch.Config{
		Addresses: []string{"http://localhost:9201"},
	})

	refSchema := schema.Schema{
		Things: &models.Schema{
			Classes: []*models.Class{
				&models.Class{
					Class: "Planet",
					Properties: []*models.Property{
						&models.Property{
							Name:     "name",
							DataType: []string{string(schema.DataTypeString)},
						},
					},
				},
				&models.Class{
					Class: "Continent",
					Properties: []*models.Property{
						&models.Property{
							Name:     "name",
							DataType: []string{string(schema.DataTypeString)},
						},
						&models.Property{
							Name:     "onPlanet",
							DataType: []string{"Planet"},
						},
					},
				},
				&models.Class{
					Class: "Country",
					Properties: []*models.Property{
						&models.Property{
							Name:     "name",
							DataType: []string{string(schema.DataTypeString)},
						},
						&models.Property{
							Name:     "onContinent",
							DataType: []string{"Continent"},
						},
					},
				},
				&models.Class{
					Class: "City",
					Properties: []*models.Property{
						&models.Property{
							Name:     "name",
							DataType: []string{string(schema.DataTypeString)},
						},
						&models.Property{
							Name:     "inCountry",
							DataType: []string{"Country"},
						},
					},
				},
				&models.Class{
					Class: "Place",
					Properties: []*models.Property{
						&models.Property{
							Name:     "name",
							DataType: []string{string(schema.DataTypeString)},
						},
						&models.Property{
							Name:     "inCity",
							DataType: []string{"City"},
						},
					},
				},
			},
		},
	}
	require.Nil(t, err)
	waitForEsToBeReady(t, client)
	logger, _ := test.NewNullLogger()
	schemaGetter := &fakeSchemaGetter{schema: refSchema}
	repo := NewRepo(client, logger, schemaGetter, 3)
	migrator := NewMigrator(repo)

	t.Run("adding all classes to the schema", func(t *testing.T) {
		for _, class := range refSchema.Things.Classes {
			t.Run(fmt.Sprintf("add %s", class.Class), func(t *testing.T) {
				err := migrator.AddClass(context.Background(), kind.Thing, class)
				require.Nil(t, err)
			})
		}
	})

	t.Run("importing some thing objects with references", func(t *testing.T) {
		objects := []models.Thing{
			models.Thing{
				Class: "Planet",
				Schema: map[string]interface{}{
					"name": "Earth",
				},
				ID:               "32c69af9-cbbe-4ec9-bf6c-365cd6c22fdf",
				CreationTimeUnix: 1566464889,
			},
			models.Thing{
				Class: "Continent",
				Schema: map[string]interface{}{
					"name": "North America",
					"onPlanet": models.MultipleRef{
						&models.SingleRef{
							Beacon: "weaviate://localhost/things/32c69af9-cbbe-4ec9-bf6c-365cd6c22fdf",
						},
					},
				},
				ID:               "4aad8154-e7f3-45b8-81a6-725171419e55",
				CreationTimeUnix: 1566464892,
			},
			models.Thing{
				Class: "Country",
				Schema: map[string]interface{}{
					"name": "USA",
					"onContinent": models.MultipleRef{
						&models.SingleRef{
							Beacon: "weaviate://localhost/things/4aad8154-e7f3-45b8-81a6-725171419e55",
						},
					},
				},
				ID:               "18c80a16-346a-477d-849d-9d92e5040ac9",
				CreationTimeUnix: 1566464896,
			},
			models.Thing{
				Class: "City",
				Schema: map[string]interface{}{
					"name": "San Francisco",
					"inCountry": models.MultipleRef{
						&models.SingleRef{
							Beacon: "weaviate://localhost/things/18c80a16-346a-477d-849d-9d92e5040ac9",
						},
					},
				},
				ID:               "2297e094-6218-43d4-85b1-3d20af752f23",
				CreationTimeUnix: 1566464899,
			},
			models.Thing{
				Class: "Place",
				Schema: map[string]interface{}{
					"name": "Tim Apple's Fruit Bar",
					"inCity": models.MultipleRef{
						&models.SingleRef{
							Beacon: "weaviate://localhost/things/2297e094-6218-43d4-85b1-3d20af752f23",
						},
					},
				},
				ID:               "4ef47fb0-3cf5-44fc-b378-9e217dff13ac",
				CreationTimeUnix: 1566464904,
			},
		}

		for _, thing := range objects {
			t.Run(fmt.Sprintf("add %s", thing.ID), func(t *testing.T) {
				err := repo.PutThing(context.Background(), &thing, []float32{1, 2, 3, 4, 5, 6, 7})
				require.Nil(t, err)
			})
		}

		// wait for index to become available
		time.Sleep(1 * time.Second)

		var before *search.Result
		t.Run("resolving the place before we have cache", func(t *testing.T) {
			res, err := repo.ThingByID(context.Background(), "4ef47fb0-3cf5-44fc-b378-9e217dff13ac", 100)
			require.Nil(t, err)
			assert.Equal(t, false, res.CacheHot)
			before = res
		})

		t.Run("init caching state machine", func(t *testing.T) {
			repo.InitCacheIndexing(50, 200*time.Millisecond, 200*time.Millisecond)
		})

		// wait for changes to take effect
		time.Sleep(1 * time.Second)

		t.Run("all 3 (outer) things must now have a hot cache", func(t *testing.T) {
			res, err := repo.ThingByID(context.Background(), "18c80a16-346a-477d-849d-9d92e5040ac9", 0)
			require.Nil(t, err)
			assert.Equal(t, true, res.CacheHot)

			res, err = repo.ThingByID(context.Background(), "18c80a16-346a-477d-849d-9d92e5040ac9", 0)
			require.Nil(t, err)
			assert.Equal(t, true, res.CacheHot)

			res, err = repo.ThingByID(context.Background(), "4ef47fb0-3cf5-44fc-b378-9e217dff13ac", 0)
			require.Nil(t, err)
			assert.Equal(t, true, res.CacheHot)
		})

		t.Run("resolving the place", func(t *testing.T) {
			res, err := repo.ThingByID(context.Background(), "4ef47fb0-3cf5-44fc-b378-9e217dff13ac", 100)
			require.Nil(t, err)

			assert.Equal(t, before.Schema, res.Schema, "result without a cache and with a cache should look the same")
		})

		t.Run("adding a new place to verify idnexing is constantly happening in the background", func(t *testing.T) {
			newPlace := models.Thing{
				Class: "Place",
				Schema: map[string]interface{}{
					"name": "John Oliver's Avocados",
					"inCity": models.MultipleRef{
						&models.SingleRef{
							Beacon: "weaviate://localhost/things/2297e094-6218-43d4-85b1-3d20af752f23",
						},
					},
				},
				ID:               "0f02d525-902d-4dc0-8052-647cb420c1a6",
				CreationTimeUnix: 1566464912,
			}

			err := repo.PutThing(context.Background(), &newPlace, []float32{1, 2, 3, 4, 5, 6, 7})
			require.Nil(t, err)
		})

		// wait for both es indexing as well as esvector caching to be complete
		time.Sleep(2500 * time.Millisecond)
	})

	t.Run("the newly added place must have a hot cache by now", func(t *testing.T) {
		res, err := repo.ThingByID(context.Background(), "0f02d525-902d-4dc0-8052-647cb420c1a6", 0)
		require.Nil(t, err)
		assert.Equal(t, true, res.CacheHot)
	})
}
