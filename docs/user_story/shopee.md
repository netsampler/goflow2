Since July 2024, the Network Development & Reliability Engineering (NDRE) team at Shopee has adopted goflow2 as the core component of its traffic sampling and analysis platform.

Based on internal requirements, we extended and modified the original architecture of goflow2. In particular, we added service tagging for internal IP traffic, which integrates with multiple internal CMDB systems to enrich flow records with business-level metadata.

Our traffic sampling is centered around dedicated network links and spans approximately **30 data centers across Southeast Asia and the Americas**. Currently, around **100 network switches** (including **Cisco, Huawei, and Arista**) export flow data to goflow2 using **NetFlow v9, NetStream, and IPFIX**.

The system uses Kafka + Apache Druid + Grafana as the storage and analytics pipeline. Flow data is **aggregated at a minute-level granularity** and stored for real-time and historical analysis.
Currently, the platform processes approximately **1–2 TB of flow records per day**, including:

- 5-tuple information
- Internal traffic
- Internet-bound traffic from isp
- Various internally defined service and business tags

The collected data is actively used in the following production scenarios:

1. **Traffic visibility**.
   Network engineers use Grafana dashboards to quickly understand the business composition of traffic under specific conditions.

2. **Network troubleshooting**.
   Flow data serves as an important reference for diagnosing and analyzing network issues.

3. **Cost allocation for dedicated links**.
   By combining traffic data with dedicated circuit cost information, we allocate network costs down to specific business services and generate monthly cost reports for management.
   This scenario places strong requirements on sampling accuracy, which we improve by tuning switch-side sampling configurations.

goflow2 has proven to be a stable and scalable foundation for large-scale, multi-vendor flow collection and analysis in our production environment.