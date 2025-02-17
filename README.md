# NetDevOps CLI Tool

## 🚀 Overview
NetDevOps CLI is a powerful command-line tool designed to automate network deployments using Terraform and Ansible. It simplifies the process of managing on-prem and cloud networking by integrating infrastructure provisioning, configuration management, and real-time monitoring.

### **🌟 MVP Focus: GNS3 Provider**
The initial version of NetDevOps CLI focuses on automating **GNS3 network deployments** with Terraform.

## 🎯 Features (MVP Phase)
✅ Deploy routers and switches in **GNS3** using Terraform
✅ Auto-generate **Terraform configurations** dynamically
✅ Support **YAML-based topology definitions**
✅ Apply **Ansible playbooks** for network configuration
✅ Interactive CLI support for user-friendly operations

## 🛠 Installation
### **1️⃣ Clone the Repository**
```sh
git clone https://github.com/your-username/netdevops-cli-tool.git
cd netdevops-cli-tool
```

### **2️⃣ Install Dependencies**
```sh
go mod tidy
```

### **3️⃣ Build the CLI**
```sh
go build -o netdevops
```

## 🚀 Usage
### **🔹 Initialize Terraform**
```sh
./netdevops init
```

### **🔹 Deploy GNS3 Network**
```sh
./netdevops deploy --routers 3 --image csr1000v
```
This deploys **3 routers** in GNS3 using the **CSR1000v** image.

### **🔹 Destroy Network**
```sh
./netdevops destroy
```

### **🔹 Deploy from YAML**
```sh
./netdevops deploy-yaml --config topology.yaml
```

## 🏗 Roadmap
- [ ] Complete Terraform integration for GNS3 ✅
- [ ] Add support for AWS networking 🌎
- [ ] Implement AI-powered troubleshooting 🤖
- [ ] Support Kubernetes-based network deployments 🏗

## 📜 License
This project is licensed under the MIT License.

## 🤝 Contributing
Pull requests are welcome! Feel free to **submit issues** and feature requests.

