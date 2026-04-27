--
-- PostgreSQL database dump
--

\restrict TO5yailhOttzRfJeTjYjRKLruJCGtdSEvt0rDLs5L7odcmzcngnJlCI2bivhEgH

-- Dumped from database version 18.3
-- Dumped by pg_dump version 18.3

SET statement_timeout = 0;
SET lock_timeout = 0;
SET idle_in_transaction_session_timeout = 0;
SET transaction_timeout = 0;
SET client_encoding = 'UTF8';
SET standard_conforming_strings = on;
SELECT pg_catalog.set_config('search_path', '', false);
SET check_function_bodies = false;
SET xmloption = content;
SET client_min_messages = warning;
SET row_security = off;

SET default_tablespace = '';

SET default_table_access_method = heap;

--
-- Name: user_login_stats; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.user_login_stats (
    user_id uuid NOT NULL,
    role character varying(32) NOT NULL,
    total_logins bigint DEFAULT 0 NOT NULL,
    successful_logins bigint DEFAULT 0 NOT NULL,
    failed_logins bigint DEFAULT 0 NOT NULL,
    last_login_at_utc timestamp with time zone,
    total_session_duration_seconds bigint DEFAULT 0 NOT NULL,
    completed_sessions_count bigint DEFAULT 0 NOT NULL,
    avg_session_duration_seconds double precision DEFAULT 0 NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL
);


--
-- Name: user_login_stats user_login_stats_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.user_login_stats
    ADD CONSTRAINT user_login_stats_pkey PRIMARY KEY (user_id, role);


--
-- PostgreSQL database dump complete
--

\unrestrict TO5yailhOttzRfJeTjYjRKLruJCGtdSEvt0rDLs5L7odcmzcngnJlCI2bivhEgH

