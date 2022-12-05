// Copyright (c) HashiCorp, Inc
// SPDX-License-Identifier: MPL-2.0
import { Construct } from "constructs";
import { App, TerraformStack, CloudBackend, NamedCloudWorkspace, TerraformAsset, AssetType } from "cdktf";
import * as google from '@cdktf/provider-google';
import * as path from 'path';

const project = 'ubiquitous-couscous';
const region = 'asia-northeast1';
// const repository = 'ubiquitous-couscous';

class MyStack extends TerraformStack {
  constructor(scope: Construct, id: string) {
    super(scope, id);

    new google.provider.GoogleProvider(this, 'google', {
      project,
      region,
    });

    const service_runner = new google.serviceAccount.ServiceAccount(this, 'service-runner', {
      accountId: 'service-runner',
    });

    new google.projectIamBinding.ProjectIamBinding(this, 'allow-secret-manager', {
      members: [`serviceAccount:${service_runner.email}`],
      project,
      role: 'roles/secretmanager.secretAccessor',
    });

    const wait_process = new google.pubsubTopic.PubsubTopic(this, 'wait-process', {
      name: 'wait-process',
    });    

    const wait_send = new google.pubsubTopic.PubsubTopic(this, 'wait-send', {
      name: 'wait-send',
    });

    const channel_access_token = new google.secretManagerSecret.SecretManagerSecret(this, 'channel-access-token', {
      secretId: 'channel-access-token',
      replication: {
        automatic: true,
      },
    });

    const function_asset = new TerraformAsset(this, 'function-asset', {
      path: path.resolve('function'),
      type: AssetType.ARCHIVE,
    });

    const function_bucket = new google.storageBucket.StorageBucket(this, 'function-bucket', {
      location: region,
      name: `source-${project}`,
      lifecycleRule: [{
        condition: {
          age: 1,
        },
        action: {
          type: 'Delete',
        },
      }],
    });

    const function_object = new google.storageBucketObject.StorageBucketObject(this, 'function-object', {
      bucket: function_bucket.name,
      name: `${function_asset.assetHash}.zip`,
      source: function_asset.path,
    });

    const receive_function = new google.cloudfunctions2Function.Cloudfunctions2Function(this, 'receive-function', {
      buildConfig: {
        runtime: 'go119',
        entryPoint: 'receive',
        source: {
          storageSource: {
            bucket: function_bucket.name,
            object: function_object.name,
          },
        },
      },
      location: region,
      name: 'receive-function',
      serviceConfig: {
        environmentVariables: {
          'WAIT_PROCESS_TOPIC': wait_process.name,
        },
        minInstanceCount: 0,
        maxInstanceCount: 1,
        serviceAccountEmail: service_runner.email,
      },
    });

    const cloudrun_noauth = new google.dataGoogleIamPolicy.DataGoogleIamPolicy(this, 'cloudrun-noauth', {
      binding: [{
        members: ['allUsers'],
        role: 'roles/run.invoker',
      }],
    });

    new google.cloudRunServiceIamPolicy.CloudRunServiceIamPolicy(this, 'receive-noauth', {
      location: region,
      policyData: cloudrun_noauth.policyData,
      service: receive_function.name,
    });

    new google.cloudfunctions2Function.Cloudfunctions2Function(this, 'process-function', {
      buildConfig: {
        runtime: 'go119',
        entryPoint: 'process',
        source: {
          storageSource: {
            bucket: function_bucket.name,
            object: function_object.name,
          },
        },
      },
      eventTrigger: {
        eventType: 'google.cloud.pubsub.topic.v1.messagePublished',
        pubsubTopic: wait_process.id,
      },
      location: region,
      name: 'process-function',
      serviceConfig: {
        environmentVariables: {
          'WAIT_SEND_TOPIC': wait_send.name,
        },
        ingressSettings: 'ALLOW_INTERNAL_ONLY',
        minInstanceCount: 0,
        maxInstanceCount: 1,
        serviceAccountEmail: service_runner.email,
      },      
    });

    new google.cloudfunctions2Function.Cloudfunctions2Function(this, 'send-function', {
      buildConfig: {
        runtime: 'go119',
        entryPoint: 'send',
        source: {
          storageSource: {
            bucket: function_bucket.name,
            object: function_object.name,
          },
        },
      },
      eventTrigger: {
        eventType: 'google.cloud.pubsub.topic.v1.messagePublished',
        pubsubTopic: wait_send.id,
      },
      location: region,
      name: 'send-function',
      serviceConfig: {
        environmentVariables: {
          'CHANNEL_ACCESS_TOKEN': channel_access_token.name,
        },
        ingressSettings: 'ALLOW_INTERNAL_ONLY',
        minInstanceCount: 0,
        maxInstanceCount: 1,
        serviceAccountEmail: service_runner.email,
      },      
    });

  }
}

const app = new App();
const stack = new MyStack(app, "ubiquitous-couscous");
new CloudBackend(stack, {
  hostname: "app.terraform.io",
  organization: "hsmtkkdefault",
  workspaces: new NamedCloudWorkspace("ubiquitous-couscous")
});
app.synth();
