# distributed_setting
# trainticket-distribute
別リポジトリにあるtrainticket-ipamに設定ファイルがあり、それを元にコンテナを任意のノードにデプロイする。
それぞれのAPIは以下の通り

・/no_proxy_no_mysql
データベース以外のサービスをデプロイする
・/no_proxy_mysql
データベースをデプロイする
・/envoy
サービスとEnvoyプロキシをデプロイして、サイドカーを実現する
・
・
・
・
・
