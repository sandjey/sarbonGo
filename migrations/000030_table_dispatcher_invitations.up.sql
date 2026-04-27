--
-- PostgreSQL database dump
--

\restrict pCw54Cbuc9oIUPU3Pc1Ar6McO3Zss7v5S8Dgso26lwzMbLPgMc7ALCVzeq79Fos

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
-- Name: dispatcher_invitations; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.dispatcher_invitations (
    id uuid DEFAULT public.uuid_generate_v4() NOT NULL,
    token character varying(64) NOT NULL,
    company_id uuid NOT NULL,
    role character varying(50) NOT NULL,
    phone character varying(20) NOT NULL,
    invited_by uuid NOT NULL,
    expires_at timestamp without time zone NOT NULL,
    created_at timestamp without time zone DEFAULT now() NOT NULL
);


--
-- Name: dispatcher_invitations dispatcher_invitations_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.dispatcher_invitations
    ADD CONSTRAINT dispatcher_invitations_pkey PRIMARY KEY (id);


--
-- Name: dispatcher_invitations dispatcher_invitations_token_key; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.dispatcher_invitations
    ADD CONSTRAINT dispatcher_invitations_token_key UNIQUE (token);


--
-- Name: idx_dispatcher_invitations_company; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_dispatcher_invitations_company ON public.dispatcher_invitations USING btree (company_id);


--
-- Name: idx_dispatcher_invitations_phone; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_dispatcher_invitations_phone ON public.dispatcher_invitations USING btree (phone);


--
-- Name: idx_dispatcher_invitations_token; Type: INDEX; Schema: public; Owner: -
--

CREATE UNIQUE INDEX idx_dispatcher_invitations_token ON public.dispatcher_invitations USING btree (token);


--
-- Name: dispatcher_invitations dispatcher_invitations_company_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.dispatcher_invitations
    ADD CONSTRAINT dispatcher_invitations_company_id_fkey FOREIGN KEY (company_id) REFERENCES public.companies(id) ON DELETE CASCADE;


--
-- PostgreSQL database dump complete
--

\unrestrict pCw54Cbuc9oIUPU3Pc1Ar6McO3Zss7v5S8Dgso26lwzMbLPgMc7ALCVzeq79Fos

