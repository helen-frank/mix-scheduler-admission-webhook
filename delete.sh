kubectl delete -f ./deployment/deployment.yaml.template
kubectl delete ns mix-scheduler-system
kubectl delete mutatingwebhookconfigurations.admissionregistration.k8s.io demo-webhook
