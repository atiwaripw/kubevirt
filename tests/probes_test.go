package tests_test

import (
	. "github.com/onsi/ginkgo"
	"github.com/onsi/ginkgo/extensions/table"
	. "github.com/onsi/gomega"
	v1 "k8s.io/api/core/v1"
	v13 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"

	v12 "kubevirt.io/client-go/api/v1"
	"kubevirt.io/client-go/kubecli"
	"kubevirt.io/kubevirt/tests"
	cd "kubevirt.io/kubevirt/tests/containerdisk"
)

var _ = Describe("[ref_id:1182]Probes", func() {
	var err error
	var virtClient kubecli.KubevirtClient

	BeforeEach(func() {
		virtClient, err = kubecli.GetKubevirtClient()
		tests.PanicOnError(err)

		tests.BeforeTestCleanup()
	})

	Context("for readiness", func() {
		var vmi *v12.VirtualMachineInstance

		const (
			period         = 5
			initialSeconds = 5
			port           = 1500
		)

		tcpProbe := createTCPProbe(period, initialSeconds, port)
		httpProbe := createHTTPProbe(period, initialSeconds, port)

		isVMIReady := func() bool {
			readVmi, err := virtClient.VirtualMachineInstance(vmi.Namespace).Get(vmi.Name, &v13.GetOptions{})
			Expect(err).ToNot(HaveOccurred())
			return vmiReady(readVmi) == v1.ConditionTrue
		}

		table.DescribeTable("should succeed", func(readinessProbe *v12.Probe, serverStarter func(vmi *v12.VirtualMachineInstance, port int)) {
			By("Specifying a VMI with a readiness probe")
			vmi = createReadyCirrosVMIWithReadinessProbe(virtClient, readinessProbe)

			// pod is not ready until our probe contacts the server
			assertPodNotReady(virtClient, vmi)

			By("Starting the server inside the VMI")
			serverStarter(vmi, 1500)

			By("Checking that the VMI and the pod will be marked as ready to receive traffic")
			Eventually(isVMIReady, 60, 1).Should(Equal(true))
			Expect(tests.PodReady(tests.GetRunningPodByVirtualMachineInstance(vmi, tests.NamespaceTestDefault))).To(Equal(v1.ConditionTrue))
		},
			table.Entry("[test_id:1202][posneg:positive]with working TCP probe and tcp server", tcpProbe, tests.StartTCPServer),
			table.Entry("[test_id:1200][posneg:positive]with working HTTP probe and http server", httpProbe, tests.StartHTTPServer),
		)

		table.DescribeTable("should fail", func(readinessProbe *v12.Probe) {
			By("Specifying a VMI with a readiness probe")
			vmi = createReadyCirrosVMIWithReadinessProbe(virtClient, readinessProbe)

			// pod is not ready until our probe contacts the server
			assertPodNotReady(virtClient, vmi)

			By("Checking that the VMI and the pod will consistently stay in a not-ready state")
			Consistently(isVMIReady).Should(Equal(false))
			Expect(tests.PodReady(tests.GetRunningPodByVirtualMachineInstance(vmi, tests.NamespaceTestDefault))).To(Equal(v1.ConditionFalse))
		},
			table.Entry("[test_id:1220][posneg:negative]with working TCP probe and no running server", tcpProbe),
			table.Entry("[test_id:1219][posneg:negative]with working HTTP probe and no running server", httpProbe),
		)
	})

	Context("for liveness", func() {
		const (
			period         = 5
			initialSeconds = 90
			port           = 1500
		)

		tcpProbe := createTCPProbe(period, initialSeconds, port)
		httpProbe := createHTTPProbe(period, initialSeconds, port)

		table.DescribeTable("should not fail the VMI", func(livenessProbe *v12.Probe, serverStarter func(vmi *v12.VirtualMachineInstance, port int)) {
			By("Specifying a VMI with a liveness probe")
			vmi := createReadyCirrosVMIWithLivenessProbe(virtClient, livenessProbe)

			By("Starting the server inside the VMI")
			serverStarter(vmi, 1500)

			By("Checking that the VMI is still running after a minute")
			Consistently(func() bool {
				vmi, err := virtClient.VirtualMachineInstance(tests.NamespaceTestDefault).Get(vmi.Name, &v13.GetOptions{})
				Expect(err).ToNot(HaveOccurred())
				return vmi.IsFinal()
			}, 120, 1).Should(Not(BeTrue()))
		},
			table.Entry("[test_id:1199][posneg:positive]with working TCP probe and tcp server", tcpProbe, tests.StartTCPServer),
			table.Entry("[test_id:1201][posneg:positive]with working HTTP probe and http server", httpProbe, tests.StartHTTPServer),
		)

		table.DescribeTable("should fail the VMI", func(livenessProbe *v12.Probe) {
			By("Specifying a VMI with a livenessProbe probe")
			vmi := createReadyCirrosVMIWithLivenessProbe(virtClient, livenessProbe)

			By("Checking that the VMI is in a final state after a minute")
			Eventually(func() bool {
				vmi, err := virtClient.VirtualMachineInstance(tests.NamespaceTestDefault).Get(vmi.Name, &v13.GetOptions{})
				Expect(err).ToNot(HaveOccurred())
				return vmi.IsFinal()
			}, 120, 1).Should(BeTrue())
		},
			table.Entry("[test_id:1217][posneg:negative]with working TCP probe and no running server", tcpProbe),
			table.Entry("[test_id:1218][posneg:negative]with working HTTP probe and no running server", httpProbe),
		)
	})
})

func createReadyCirrosVMIWithReadinessProbe(virtClient kubecli.KubevirtClient, probe *v12.Probe) *v12.VirtualMachineInstance {
	dummyUserData := "#!/bin/bash\necho 'hello'\n"
	vmi := tests.NewRandomVMIWithEphemeralDiskAndUserdata(
		cd.ContainerDiskFor(cd.ContainerDiskCirros), dummyUserData)
	vmi.Spec.ReadinessProbe = probe

	return createAndBlockUntilVMIHasStarted(virtClient, vmi)
}

func createReadyCirrosVMIWithLivenessProbe(virtClient kubecli.KubevirtClient, probe *v12.Probe) *v12.VirtualMachineInstance {
	dummyUserData := "#!/bin/bash\necho 'hello'\n"
	vmi := tests.NewRandomVMIWithEphemeralDiskAndUserdata(
		cd.ContainerDiskFor(cd.ContainerDiskCirros), dummyUserData)
	vmi.Spec.LivenessProbe = probe

	return createAndBlockUntilVMIHasStarted(virtClient, vmi)
}

func createAndBlockUntilVMIHasStarted(virtClient kubecli.KubevirtClient, vmi *v12.VirtualMachineInstance) *v12.VirtualMachineInstance {
	_, err := virtClient.VirtualMachineInstance(tests.NamespaceTestDefault).Create(vmi)
	Expect(err).ToNot(HaveOccurred())

	// It may come to modify retries on the VMI because of the kubelet updating the pod, which can trigger controllers more often
	tests.WaitForSuccessfulVMIStartIgnoreWarnings(vmi)

	// read back the created VMI, so it has the UID available on it
	startedVMI, err := virtClient.VirtualMachineInstance(tests.NamespaceTestDefault).Get(vmi.Name, &v13.GetOptions{})
	Expect(err).ToNot(HaveOccurred())
	return startedVMI
}

func vmiReady(vmi *v12.VirtualMachineInstance) v1.ConditionStatus {
	for _, cond := range vmi.Status.Conditions {
		if cond.Type == v12.VirtualMachineInstanceConditionType(v1.PodReady) {
			return cond.Status
		}
	}
	return v1.ConditionFalse
}

func assertPodNotReady(virtClient kubecli.KubevirtClient, vmi *v12.VirtualMachineInstance) {
	Expect(tests.PodReady(tests.GetRunningPodByVirtualMachineInstance(vmi, tests.NamespaceTestDefault))).To(Equal(v1.ConditionFalse))
	readVmi, err := virtClient.VirtualMachineInstance(vmi.Namespace).Get(vmi.Name, &v13.GetOptions{})
	Expect(err).ToNot(HaveOccurred())
	Expect(vmiReady(readVmi)).To(Equal(v1.ConditionFalse))
}

func createTCPProbe(period int32, initialSeconds int32, port int) *v12.Probe {
	httpHandler := v12.Handler{
		TCPSocket: &v1.TCPSocketAction{
			Port: intstr.FromInt(port),
		},
	}
	return createProbeSpecification(period, initialSeconds, httpHandler)
}

func createHTTPProbe(period int32, initialSeconds int32, port int) *v12.Probe {
	httpHandler := v12.Handler{
		HTTPGet: &v1.HTTPGetAction{
			Port: intstr.FromInt(port),
		},
	}
	return createProbeSpecification(period, initialSeconds, httpHandler)
}

func createProbeSpecification(period int32, initialSeconds int32, handler v12.Handler) *v12.Probe {
	return &v12.Probe{
		PeriodSeconds:       period,
		InitialDelaySeconds: initialSeconds,
		Handler:             handler,
	}
}
