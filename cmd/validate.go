package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"text/tabwriter"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"
	diff "github.com/yudai/gojsondiff"
	"github.com/yudai/gojsondiff/formatter"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/runtime/serializer"
	"k8s.io/client-go/tools/clientcmd"

	espejov1alpha1 "github.com/vshn/espejote/api/v1alpha1"
	"github.com/vshn/espejote/internal/controller/converter"
	"github.com/vshn/espejote/internal/dynamic"
)

const (
	crdClusterwide = "ClusterManagedResource"
	crdNamespaced  = "ManagedResource"
)

func ValidateContext(cmd *cobra.Command, args []string) {
	clt := getDynamicClient()
	obj, gvk := getManagedResource(args[0])

	switch gvk.Kind {
	case crdNamespaced:
		// 👇 Implement the thing
		fmt.Println("validating namespaced ManagedResource not yet supported")

	case crdClusterwide:
		mr := obj.(*espejov1alpha1.ClusterManagedResource)
		p, err := converter.ToParser(context.Background(), clt, mr)
		if err != nil {
			log.Error(err, "Converting ManagedResource to parser")
			os.Exit(1)
		}

		w := tabwriter.NewWriter(os.Stdout, 0, 0, 3, ' ', 0)
		quickErr2(fmt.Fprintln(w, "ALIAS\tKIND\tNAMESPACE\tNAME"))
		for k, v := range p.Input {
			for _, i := range v.Items {
				quickErr2(fmt.Fprintf(w, "%s\t%s\t%s\t%s\n", k, strings.ToLower(i.GetKind()), i.GetNamespace(), i.GetName()))
			}
		}

		quickErr(w.Flush())

	default:
		quickErr2(fmt.Printf("%s doesn't contain a manifest of group espejo.appuio.io/v1alpha1\n", args[0]))
	}
}

func ValidateNamespace(cmd *cobra.Command, args []string) {
	clt := getDynamicClient()
	obj, gvk := getManagedResource(args[0])

	switch gvk.Kind {
	case crdNamespaced:
		fmt.Println("validating selector in namespaced ManagedResource not possible")

	case crdClusterwide:
		mr := obj.(*espejov1alpha1.ClusterManagedResource)
		p, err := converter.ToParser(context.Background(), clt, mr)
		if err != nil {
			log.Error(err, "Converting ManagedResource to parser")
			os.Exit(1)
		}

		w := tabwriter.NewWriter(os.Stdout, 0, 0, 3, ' ', 0)
		quickErr2(fmt.Fprintln(w, "ALIAS\tNAMESPACE"))

		// Only for ClusterManagedResource print selected namespaces
		for _, ns := range p.Namespaces.Items {
			quickErr2(fmt.Fprintf(w, "namespace\t%s\n", ns.GetName()))
		}

		quickErr(w.Flush())

	default:
		fmt.Printf("%s doesn't contain a manifest of group espejo.appuio.io/v1alpha1\n", args[0])
	}
}

func ValidateTemplate(cmd *cobra.Command, args []string) {
	clt := getDynamicClient()
	obj, gvk := getManagedResource(args[0])

	switch gvk.Kind {
	case crdNamespaced:
		fmt.Println("validating namespaced ManagedResource not yet supported")

	case crdClusterwide:
		mr := obj.(*espejov1alpha1.ClusterManagedResource)
		p, err := converter.ToParser(context.Background(), clt, mr)
		if err != nil {
			log.Error(err, "Converting ManagedResource to parser")
			os.Exit(1)
		}

		// output, err := p.ParseToJson()
		// if err != nil {
		// 	log.Error(err, "Parsing Jsonnet template")
		// 	os.Exit(1)
		// }

		// for _, o := range output {
		// 	fmt.Println(o)
		// }

		// 🤮 Now thats a mess here, below the line
		// ---------------------------------------------------------------------

		output, err := p.Parse()
		if err != nil {
			log.Error(err, "Parsing Jsonnet template")
			os.Exit(1)
		}

		for _, r := range output {
			// decode to json
			right, err := json.MarshalIndent(r, "", "   ")
			if err != nil {
				log.Error(err, "Printing Diff")
				continue
			}

			// dynamic resource
			dr, err := clt.Resource(r.GetAPIVersion(), r.GetKind(), r.GetNamespace())
			if err != nil {
				log.Error(err, "Printing Diff")
				os.Exit(1)
			}

			// get object from cluster
			l, err := dr.Get(context.Background(), r.GetName(), metav1.GetOptions{})
			if err != nil {
				fmt.Println(string(right))
				continue
			}

			left, _ := json.MarshalIndent(l, "", "   ")

			differ := diff.New()
			d, _ := differ.Compare(left, right)

			var aJson map[string]interface{}
			quickErr(json.Unmarshal(left, &aJson))

			formatter := formatter.NewAsciiFormatter(aJson, formatter.AsciiFormatterConfig{
				ShowArrayIndex: false,
				Coloring:       true,
			})
			diffString, err := formatter.Format(d)
			if err != nil {
				log.Error(err, "Do De Diff")
			}
			fmt.Println(diffString)
		}

	default:
		fmt.Printf("%s doesn't contain a manifest of group espejo.appuio.io/v1alpha1\n", args[0])
	}
}

func getDynamicClient() *dynamic.Client {
	// use the current context in kubeconfig
	config, err := clientcmd.BuildConfigFromFlags("", viper.GetString("kubeconfig"))
	if err != nil {
		log.Error(err, "Creating dynamic client")
		os.Exit(1)
	}

	// return the client
	clt, err := dynamic.NewForConfig(config)
	if err != nil {
		log.Error(err, "Creating dynamic client")
		os.Exit(1)
	}

	return clt
}

func getManagedResource(file string) (runtime.Object, *schema.GroupVersionKind) {
	var decode = serializer.NewCodecFactory(scheme).UniversalDeserializer().Decode

	_, err := os.Stat(file)
	if err != nil {
		log.Error(err, "Creating dynamic client")
		os.Exit(1)
	}

	data, err := os.ReadFile(file)
	if err != nil {
		log.Error(err, "Creating dynamic client")
		os.Exit(1)
	}

	obj, gvk, err := decode(data, nil, nil)
	if err != nil {
		os.Exit(1)
	}

	return obj, gvk
}

func quickErr(err error) {
	if err != nil {
		log.Error(err)
		os.Exit(1)
	}
}

func quickErr2(i interface{}, err error) {
	quickErr(err)
}
