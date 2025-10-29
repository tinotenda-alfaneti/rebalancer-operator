pipeline {
  agent any

  environment {
    GITHUB_USER     = "tinotenda-alfaneti"
    REPO_NAME       = "${env.JOB_NAME.split('/')[1]}"
    IMAGE_NAME      = "tinorodney/${REPO_NAME}"
    TAG             = "v${env.BUILD_NUMBER}"
    APP_NAME        = "rebalancer"
    NAMESPACE       = "${REPO_NAME}-ns"
    SOURCE_NS       = "test-ns"
    KUBECONFIG_CRED = "kubeconfigglobal"
    CONTEXT_SUBDIR  = "."
    PATH            = "$WORKSPACE/bin:$PATH"
    CACHE_DIR       = "${env.JENKINS_HOME}/cache/bin"
  }

  stages {
    stage('Checkout Code') {
      steps {
        echo "📦 Checking out ${REPO_NAME}..."
        checkout scm
        sh 'mkdir -p $WORKSPACE/bin'
      }
    }

    stage('Setup Tools') {
      steps {
        sh ''' 
        mkdir -p $CACHE_DIR

        # ======================
        # ⚙️ Install kubectl
        # ======================
        if [ ! -x "$CACHE_DIR/kubectl" ]; then
          echo "⚙️ Installing kubectl..."
          ARCH=$(uname -m)
          case "$ARCH" in
            aarch64) KARCH="arm64" ;;
            x86_64)  KARCH="amd64" ;;
            *) echo "❌ Unsupported architecture: $ARCH"; exit 1 ;;
          esac

          VER=$(curl -s https://storage.googleapis.com/kubernetes-release/release/stable.txt)
          curl -fsSLO "https://storage.googleapis.com/kubernetes-release/release/${VER}/bin/linux/${KARCH}/kubectl"
          chmod +x kubectl
          mv kubectl $CACHE_DIR/
        else
          echo "✅ Using cached kubectl"
        fi


        # ======================
        # ⚙️ Install Helm
        # ======================
        if [ ! -x "$CACHE_DIR/helm" ]; then
          echo "⚙️ Installing Helm..."
          curl -fsSL -o get_helm.sh https://raw.githubusercontent.com/helm/helm/main/scripts/get-helm-3
          chmod 700 get_helm.sh
          ./get_helm.sh
          mv $(command -v helm) $CACHE_DIR/
        else
          echo "✅ Using cached Helm"
        fi


        # ======================
        # ⚙️ Install Go
        # ======================
        if [ ! -x "$CACHE_DIR/go" ]; then
          echo "⚙️ Installing Go..."
          GOVERSION="1.22.6"
          ARCH=$(uname -m)
          case "$ARCH" in
            aarch64) GOARCH="arm64" ;;
            x86_64)  GOARCH="amd64" ;;
            *) echo "❌ Unsupported architecture: $ARCH"; exit 1 ;;
          esac

          curl -fsSLO "https://go.dev/dl/go${GOVERSION}.linux-${GOARCH}.tar.gz"
          tar -C /tmp -xzf "go${GOVERSION}.linux-${GOARCH}.tar.gz"
          mv /tmp/go/bin/go $CACHE_DIR/
          rm -rf /tmp/go "go${GOVERSION}.linux-${GOARCH}.tar.gz"
        else
          echo "✅ Using cached Go"
        fi


        # ======================
        # ✅ Add cache directory to PATH
        # ======================
        export PATH=$CACHE_DIR:$PATH

        echo "🔍 Tool versions:"
        $CACHE_DIR/kubectl version --client --short || true
        $CACHE_DIR/helm version || true
        $CACHE_DIR/go version || true
        '''
      }
    }

    stage('Unit Tests') {
      steps {
        sh '''
          export PATH=$WORKSPACE/bin:$WORKSPACE/.go/bin:$PATH
          go env -w GOPATH=$WORKSPACE/.gopath
          echo "🧪 Running unit tests..."
          go test ./...
        '''
      }
    }

    stage('Verify Cluster Access') {
      steps {
        withCredentials([file(credentialsId: "${KUBECONFIG_CRED}", variable: 'KUBECONFIG_FILE')]) {
          sh '''
            echo "🔐 Setting up kubeconfig..."
            mkdir -p $WORKSPACE/.kube
            cp "$KUBECONFIG_FILE" $WORKSPACE/.kube/config
            chmod 600 $WORKSPACE/.kube/config
            export KUBECONFIG=$WORKSPACE/.kube/config
            $WORKSPACE/bin/kubectl cluster-info
          '''
        }
      }
    }

    stage('Prepare Namespace & Secrets') {
      steps {
        withCredentials([file(credentialsId: "${KUBECONFIG_CRED}", variable: 'KUBECONFIG_FILE')]) {
          sh '''
            export KUBECONFIG=$WORKSPACE/.kube/config
            echo "🧱 Ensuring namespace ${NAMESPACE} exists..."
            $WORKSPACE/bin/kubectl create namespace ${NAMESPACE} --dry-run=client -o yaml | $WORKSPACE/bin/kubectl apply -f -

            echo "🔁 Copying dockerhub credentials..."
            $WORKSPACE/bin/kubectl get secret dockerhub-creds -n ${SOURCE_NS} -o yaml \
              | grep -v 'resourceVersion:' | grep -v 'uid:' | grep -v 'creationTimestamp:' \
              | sed "s/namespace: ${SOURCE_NS}/namespace: ${NAMESPACE}/" \
              | $WORKSPACE/bin/kubectl apply -n ${NAMESPACE} -f -

            echo "🔐 Ensuring kaniko-builder ServiceAccount and RBAC exist..."
            $WORKSPACE/bin/kubectl get sa kaniko-builder -n ${NAMESPACE} >/dev/null 2>&1 \
              || $WORKSPACE/bin/kubectl create serviceaccount kaniko-builder -n ${NAMESPACE}

            CRB_NAME="kaniko-builder-${NAMESPACE}"
            if ! $WORKSPACE/bin/kubectl get clusterrolebinding ${CRB_NAME} >/dev/null 2>&1; then
              $WORKSPACE/bin/kubectl create clusterrolebinding ${CRB_NAME} \
                --clusterrole=cluster-admin \
                --serviceaccount=${NAMESPACE}:kaniko-builder
            fi
          '''
        }
      }
    }

    stage('Build Image with Kaniko') {
      steps {
        withCredentials([file(credentialsId: "${KUBECONFIG_CRED}", variable: 'KUBECONFIG_FILE')]) {
          sh '''
            export KUBECONFIG=$WORKSPACE/.kube/config
            CONTEXT_URL="git://github.com/${GITHUB_USER}/${REPO_NAME}.git"
            IMAGE_DEST="${IMAGE_NAME}:${TAG}"

            echo "🚀 Launching Kaniko build for ${IMAGE_DEST}"
            sed -e "s|__CONTEXT_URL__|${CONTEXT_URL}|g" \
                -e "s|__CONTEXT_SUBDIR__|${CONTEXT_SUBDIR}|g" \
                -e "s|__IMAGE_DEST__|${IMAGE_DEST}|g" \
                $WORKSPACE/ci/kubernetes/kaniko.yaml > kaniko-job.yaml

            $WORKSPACE/bin/kubectl delete job kaniko-job -n ${NAMESPACE} --ignore-not-found=true
            $WORKSPACE/bin/kubectl apply -f kaniko-job.yaml -n ${NAMESPACE}
            $WORKSPACE/bin/kubectl wait --for=condition=complete job/kaniko-job -n ${NAMESPACE} --timeout=20m
            $WORKSPACE/bin/kubectl logs job/kaniko-job -n ${NAMESPACE} || true
          '''
        }
      }
    }

    stage('Scan Image with Trivy') {
      steps {
        withCredentials([file(credentialsId: "${KUBECONFIG_CRED}", variable: 'KUBECONFIG_FILE')]) {
          sh '''
            export KUBECONFIG=$WORKSPACE/.kube/config
            IMAGE_DEST="${IMAGE_NAME}:${TAG}"

            echo "🔍 Running Trivy scan for ${IMAGE_DEST}"
            sed "s|__IMAGE_DEST__|${IMAGE_DEST}|g" $WORKSPACE/ci/kubernetes/trivy.yaml > trivy-job.yaml

            $WORKSPACE/bin/kubectl delete job trivy-scan -n ${NAMESPACE} --ignore-not-found=true
            $WORKSPACE/bin/kubectl apply -f trivy-job.yaml -n ${NAMESPACE}
            $WORKSPACE/bin/kubectl wait --for=condition=complete job/trivy-scan -n ${NAMESPACE} --timeout=10m || true
            $WORKSPACE/bin/kubectl logs job/trivy-scan -n ${NAMESPACE} || true
          '''
        }
      }
    }

    stage('Deploy with Helm') {
      steps {
        withCredentials([file(credentialsId: "${KUBECONFIG_CRED}", variable: 'KUBECONFIG_FILE')]) {
          sh '''
            export KUBECONFIG=$WORKSPACE/.kube/config
            echo "⚙️ Deploying ${APP_NAME} via Helm..."

            $WORKSPACE/bin/helm upgrade --install ${APP_NAME} $WORKSPACE/charts/rebalancer \
              --namespace ${NAMESPACE} \
              --create-namespace \
              --set image.repository=${IMAGE_NAME} \
              --set image.tag=${TAG} \
              --wait --timeout 5m
          '''
        }
      }
    }

    stage('Verify Deployment') {
      steps {
        withCredentials([file(credentialsId: "${KUBECONFIG_CRED}", variable: 'KUBECONFIG_FILE')]) {
          sh '''
            export KUBECONFIG=$WORKSPACE/.kube/config
            echo "🩺 Running Helm tests..."
            $WORKSPACE/bin/helm test ${APP_NAME} --namespace ${NAMESPACE} --logs --timeout 2m || true
            $WORKSPACE/bin/kubectl get pods -n ${NAMESPACE}
          '''
        }
      }
    }
  }

  post {
    success {
      echo "✅ Pipeline completed successfully."
    }
    failure {
      echo "❌ Pipeline failed."
    }
    always {
      withCredentials([file(credentialsId: "${KUBECONFIG_CRED}", variable: 'KUBECONFIG_FILE')]) {
        sh '''
          export KUBECONFIG=$WORKSPACE/.kube/config
          kubectl delete job kaniko-job -n ${NAMESPACE} --ignore-not-found=true
          kubectl delete job trivy-scan -n ${NAMESPACE} --ignore-not-found=true
        '''
      }
      cleanWs()
    }
  }
}
